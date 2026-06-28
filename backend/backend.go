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
	"errors"
	"fmt"
	"log"
	"os"
	"context"
	"os/user"
	"path"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/FractalJim/s3pop-server/mailutils"
)

const indexFileName = "_email_index.txt"


type mailFile struct {
	index    int
	filename string
}

// index management functions
// index keeps track of ids of all emails ever seen, it is never deleted from
func loadIndex(emailDir string) (filesByIndex map[int]*mailFile, filesByName map[string]*mailFile) {
	filesByIndex = make(map[int]*mailFile)
	filesByName = make(map[string]*mailFile)
	var indexFile = filepath.Join(emailDir, indexFileName)
	_, err := os.Stat(indexFile)
	if nil != err {
		//index does not exist yet or cant be opened
		return
	}

	var indexData *os.File
	indexData, err = os.Open(indexFile)
	checkError(err)
	defer func() { _ = indexData.Close() }()

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

	checkError(indexScanner.Err())
	return
}

func appendIndex(name, emailDir string, filesByIndex map[int]*mailFile, filesByName map[string]*mailFile) {
	var indexFile = filepath.Join(emailDir, indexFileName)
	var indexData *os.File
	_, err := os.Stat(indexFile)
	if nil != err {
		//index does not exist yet or cant be opened
		indexData, err = os.Create(indexFile)
	} else {
		indexData, err = os.OpenFile(indexFile, os.O_APPEND|os.O_WRONLY, 0600)
	}

	checkError(err)
	defer func() { _ = indexData.Close() }()

	_, _ = indexData.WriteString(name + "\n")
	var newID = getNextID(filesByIndex)

	var thisFile = &mailFile{
		filename: name,
		index:    newID,
	}
	filesByIndex[newID] = thisFile
	filesByName[name] = thisFile
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

func DownloadEmails(emailBucket, emailFolder string, opts ...any) error {

	client, err := getClient(opts...)
	if nil != err {
		return err
	}

	params := &s3.ListObjectsV2Input{
		Bucket: aws.String(emailBucket),
		Prefix: aws.String(emailFolder),
	}

	resp, err := client.ListObjectsV2(context.TODO(), params)
	if nil != err {
		return err
	}
	userEmailDir := mailutils.GetEmailDir(emailFolder)
	filesByIndex, filesByName := loadIndex(userEmailDir)

	for _, key := range resp.Contents {
		emailID := path.Base(*key.Key)
		_, known := filesByName[emailID]
		if !known {
			nextPopID := getNextID(filesByIndex)
			emailFile := filepath.Join(userEmailDir, emailID)
			err = downloadFile(*key.Key, emailBucket, emailFile, client)
			if nil != err {
				return err
			}
			processEmail(userEmailDir, emailID, nextPopID)
			appendIndex(emailID, userEmailDir, filesByIndex, filesByName)
		}
	}
	return nil
}

func processEmail(emailDir string, filename string, id int) {
	emailFile := filepath.Join(emailDir, filename)
	headers, body := splitEmail(emailFile)
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
	metadata.Save(emailDir)
}

func splitEmail(fullFilePath string) (headers []string, body []string) {
	fileData, err := os.Open(fullFilePath)
	checkError(err)
	defer func() { _ = fileData.Close() }()

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

	return
}

func calcPartSizeBytes(part []string) int {
	var sum int
	for _, line := range part {
		sum += len(line) + 2
	}
	return sum
}

func downloadFile(key, bucket string, outputPath string, client *s3.Client) error {

	fmt.Printf("Beginning download of %s.\n", key)
	file, err := os.Create(outputPath)
	if nil != err {
		return err
	}
	defer func() { _ = file.Close() }()

	downloader := manager.NewDownloader(client)

	_, err = downloader.Download(context.TODO(), file, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if nil != err {
		return err
	}
	_ = file.Close()
	fmt.Printf("Download of %s complete.\n", key)
	fmt.Printf("Downloaded file written to %s.\n", outputPath)

	return err
}

func getClient(opts ...any) (client *s3.Client, err error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Panic creating Client:", r)
			err = errors.New(r.(string))
		}
	}()
	var userInfo *user.User
	userInfo, err = user.Current()
	if nil != err {
		return nil, err
	}

	_, err = os.Stat(filepath.Join(userInfo.HomeDir, ".aws", "config"))
	if nil != err {
		return nil, err
	}

	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
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

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
