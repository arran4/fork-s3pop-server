package backend

/*
    s3pop-server: An AWS S3 backed POP3 server
	Copyright (C) 2018 James W Matheson
	fractal.mango@gmail.com

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as
    published by the Free Software Foundation, either version 3 of the
    License, or (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/
import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/FractalJim/s3pop-server/mailutils"
)

var (
	ErrS3Client      = errors.New("failed to get S3 client")
	ErrS3ListObjects = errors.New("failed to list S3 objects")
	ErrS3Download    = errors.New("failed to download from S3")
	ErrFileCreate    = errors.New("failed to create local file")
	ErrUserCurrent   = errors.New("failed to get current user")
	ErrAWSConfig     = errors.New("missing AWS config")
	ErrAWSLoadConfig = errors.New("failed to load AWS default config")
	ErrFileOpen      = errors.New("failed to open file")
	ErrFileWrite     = errors.New("failed to write file")
	ErrFileRead      = errors.New("failed to read file")
	ErrIndexOpen     = errors.New("failed to open index file")
	ErrIndexScanner  = errors.New("error scanning index file")
	ErrGetEmailDir   = errors.New("failed to get email directory")
	ErrLoadIndex     = errors.New("failed to load index")
	ErrProcessEmail  = errors.New("failed to process email")
)

const indexFileName = "_email_index.txt"


type mailFile struct {
	index    int
	filename string
}

// index management functions
// index keeps track of ids of all emails ever seen, it is never deleted from
func loadIndex(emailDir string) (filesByIndex map[int]*mailFile, filesByName map[string]*mailFile, err error) {
	filesByIndex = make(map[int]*mailFile)
	filesByName = make(map[string]*mailFile)
	var indexFile = filepath.Join(emailDir, indexFileName)
	_, err = os.Stat(indexFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return filesByIndex, filesByName, nil
		}
		return nil, nil, fmt.Errorf("%w: %w", ErrIndexOpen, err)
	}

	var indexData *os.File
	indexData, err = os.Open(indexFile)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrIndexOpen, err)
	}
	defer func() {
		if err := indexData.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("Error closing index data file: %v\n", err)
		}
	}()

	var indexScanner = bufio.NewScanner(indexData)
	var currentIndex int
	for indexScanner.Scan() {
		var thisFile = &mailFile{
			filename: indexScanner.Text(),
			index:    currentIndex,
		}
		filesByIndex[currentIndex] = thisFile
		filesByName[indexScanner.Text()] = thisFile
		currentIndex++
	}

	if err := indexScanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrIndexScanner, err)
	}
	return filesByIndex, filesByName, nil
}

func appendIndex(name, emailDir string, filesByIndex map[int]*mailFile, filesByName map[string]*mailFile) error {
	var indexFile = filepath.Join(emailDir, indexFileName)
	var indexData *os.File
	_, err := os.Stat(indexFile)
	if nil != err {
		//index does not exist yet or cant be opened
		indexData, err = os.Create(indexFile)
	} else {
		indexData, err = os.OpenFile(indexFile, os.O_APPEND|os.O_WRONLY, 0600)
	}

	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileOpen, err)
	}
	defer func() {
		if err := indexData.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("Error closing index data file: %v\n", err)
		}
	}()

	_, err = indexData.WriteString(name + "\n")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileWrite, err)
	}
	if err := indexData.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrFileWrite, err)
	}

	var newID = getNextID(filesByIndex)

	var thisFile = &mailFile{
		filename: name,
		index:    newID,
	}
	filesByIndex[newID] = thisFile
	filesByName[name] = thisFile

	return nil
}

func getNextID(filesByIndex map[int]*mailFile) int {
	var res int
	for key := range filesByIndex {
		if key > res {
			res = key
		}
	}
	return res + 1
}

type (
	S3Endpoint       string
	S3ForcePathStyle *bool
)

func DownloadEmails(ctx context.Context, emailBucket, emailFolder string, opts ...any) error {

	client, err := getClient(opts...)
	if nil != err {
		return fmt.Errorf("%w: %w", ErrS3Client, err)
	}

	params := &s3.ListObjectsV2Input{
		Bucket: aws.String(emailBucket),
		Prefix: aws.String(emailFolder),
	}

	resp, err := client.ListObjectsV2(ctx, params)
	if nil != err {
		return fmt.Errorf("%w: %w", ErrS3ListObjects, err)
	}
	userEmailDir, err := mailutils.GetEmailDir(emailFolder)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGetEmailDir, err)
	}
	filesByIndex, filesByName, err := loadIndex(userEmailDir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLoadIndex, err)
	}

	for _, key := range resp.Contents {
		emailID := path.Base(*key.Key)
		_, known := filesByName[emailID]
		if !known {
			nextPopID := getNextID(filesByIndex)
			emailFile := filepath.Join(userEmailDir, emailID)
			err = downloadFile(ctx, *key.Key, emailBucket, emailFile, client)
			if nil != err {
				return fmt.Errorf("%w: %w", ErrS3Download, err)
			}
			if err := processEmail(userEmailDir, emailID, nextPopID); err != nil {
				log.Printf("Error processing email %s: %v", emailID, err)
				continue
			}
			if err := appendIndex(emailID, userEmailDir, filesByIndex, filesByName); err != nil {
				log.Printf("Error appending index for %s: %v", emailID, err)
			}
		}
	}
	return nil
}

func processEmail(emailDir string, filename string, id int) error {
	emailFile := filepath.Join(emailDir, filename)
	headers, body, err := splitEmail(emailFile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProcessEmail, err)
	}
	headerSize := calcPartSizeBytes(headers)
	bodySize := calcPartSizeBytes(body)
	metadata := &mailutils.MailData{
		Name:        filename,
		ID:          id,
		Read:        false,
		HeaderSize:  headerSize,
		MessageSize: bodySize,
		TotalSize:   headerSize + bodySize,
	}
	if err := metadata.Save(emailDir); err != nil {
		return fmt.Errorf("%w: %w", ErrProcessEmail, err)
	}
	return nil
}

func splitEmail(fullFilePath string) (headers []string, body []string, err error) {
	fileData, err := os.Open(fullFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrFileOpen, err)
	}
	defer func() {
		if err := fileData.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("Error closing file: %v\n", err)
		}
	}()

	headers = make([]string, 0)
	body = make([]string, 0)
	var inHeaders = true

	fileScanner := bufio.NewScanner(fileData)

	for fileScanner.Scan() {
		if fileScanner.Text() == "" {
			if inHeaders {
				inHeaders = false
			}
		}
		if inHeaders {
			headers = append(headers, fileScanner.Text())
		} else {
			body = append(body, fileScanner.Text())
		}
	}

	if err := fileScanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrFileRead, err)
	}

	return
}

func calcPartSizeBytes(part []string) int {
	var sum int
	for _, line := range part {
		sum += len(line) + 2
	}
	return sum
}

func downloadFile(ctx context.Context, key, bucket string, outputPath string, client *s3.Client) error {

	log.Printf("Beginning download of %s.", key)
	file, err := os.Create(outputPath)
	if nil != err {
		return fmt.Errorf("%w %s: %w", ErrFileCreate, outputPath, err)
	}
	defer func() {
		if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("Error closing file: %v\n", err)
		}
	}()

	downloader := transfermanager.New(client)

	_, err = downloader.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		WriterAt: file,
	})
	if nil != err {
		return fmt.Errorf("%w %s: %w", ErrS3Download, key, err)
	}
	_ = file.Close()
	log.Printf("Download of %s complete.", key)
	log.Printf("Downloaded file written to %s.", outputPath)

	return err
}

func getClient(opts ...any) (client *s3.Client, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Panic creating Client:", r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	var userInfo *user.User
	userInfo, err = user.Current()
	if nil != err {
		return nil, fmt.Errorf("%w: %w", ErrUserCurrent, err)
	}

	_, err = os.Stat(filepath.Join(userInfo.HomeDir, ".aws", "config"))
	if nil != err {
		return nil, fmt.Errorf("%w: %w", ErrAWSConfig, err)
	}

	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAWSLoadConfig, err)
	}

	var customEndpoint *string
	var forcePathStyle *bool

	for _, opt := range opts {
		switch v := opt.(type) {
		case S3Endpoint:
			if v != "" {
				s := string(v)
				customEndpoint = &s
			}
		case S3ForcePathStyle:
			if v != nil {
				forcePathStyle = v
			}
		}
	}

	client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		if customEndpoint != nil {
			o.BaseEndpoint = customEndpoint
		}
		if forcePathStyle != nil {
			o.UsePathStyle = *forcePathStyle
		}
	})

	return
}

