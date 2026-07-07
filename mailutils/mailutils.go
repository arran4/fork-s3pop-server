package mailutils

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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
)

var (
	ErrJSONMarshal   = errors.New("failed to marshal JSON")
	ErrJSONUnmarshal = errors.New("failed to unmarshal JSON")
	ErrFileCreate    = errors.New("failed to create file")
	ErrFileWrite     = errors.New("failed to write file")
	ErrFileRead      = errors.New("failed to read file")
	ErrUserCurrent   = errors.New("failed to get current user")
	ErrDirCreate     = errors.New("failed to create directory")
)

type MailData struct {
	ID          int    `json:"id"`
	HeaderSize  int    `json:"headerSize"`
	MessageSize int    `json:"messageSize"`
	TotalSize   int    `json:"totalSize"`
	Read        bool   `json:"read"`
	Name        string `json:"name"`
}

func (m *MailData) Save(emailDir string) error {
	jsonData, err := json.Marshal(&m)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrJSONMarshal, err)
	}

	metadataFilename := filepath.Join(emailDir, m.Name+".json")
	metadataFile, err := os.Create(metadataFilename)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileCreate, err)
	}
	defer func() {
		if err := metadataFile.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("Error closing metadata file: %v\n", err)
		}
	}()

	_, err = metadataFile.Write(jsonData)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileWrite, err)
	}
	if err := metadataFile.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrFileWrite, err)
	}
	return nil
}

func LoadMailData(emailDir string, filename string) (m *MailData, err error) {
	if filepath.Ext(filename) != ".json" {
		filename += ".json"
	}
	metadataFilename := filepath.Join(emailDir, filename)
	jsonData, err := os.ReadFile(metadataFilename)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFileRead, err)
	}

	m = &MailData{Read: false}
	err = json.Unmarshal(jsonData, m)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return m, nil
}

func GetEmailDir(emailUser string) (string, error) {
	var userInfo *user.User
	userInfo, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUserCurrent, err)
	}

	dirName := filepath.Join(userInfo.HomeDir, ".email")
	_, err = os.Stat(dirName)
	if nil != err {
		err = os.Mkdir(dirName, 0700)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrDirCreate, err)
		}
	}
	emailPath := filepath.Join(dirName, emailUser)
	_, err = os.Stat(emailPath)
	if nil != err {
		err = os.Mkdir(emailPath, 0700)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrDirCreate, err)
		}
	}
	return emailPath, nil
}
