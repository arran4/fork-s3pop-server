package main

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
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/FractalJim/s3pop-server/mailutils"
)

var (
	ErrIndexOutOfRange = errors.New("index out of range")
	ErrWalkDir         = errors.New("failed to walk directory")
)

func getMessageData(emailDir string) ([]*mailutils.MailData, error) {
	var emailMetafiles []string
	err := filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error accessing path %s: %v", path, err)
			return err
		}
		if !info.IsDir() {
			if filepath.Ext(path) == ".json" {
				emailMetafiles = append(emailMetafiles, filepath.Base(path))
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWalkDir, err)
	}

	var result = make([]*mailutils.MailData, 0)
	for _, mailItem := range emailMetafiles {
		itemDetails, err := mailutils.LoadMailData(emailDir, mailItem)
		if err != nil {
			log.Printf("Error loading mail data for %s: %v", mailItem, err)
			continue
		}
		result = append(result, itemDetails)
	}
	return result, nil
}

func getStat(mailData []*mailutils.MailData, deletedItems map[int]struct{}) (count int, size int) {

	count = 0
	for id, mailItem := range mailData {
		if _, toDel := deletedItems[id]; toDel {
			continue
		}
		count++
		size += mailItem.TotalSize
	}
	return
}

func getCommand(line string) (string, []string) {
	line = strings.Trim(line, "\r \n")
	cmd := strings.Split(line, " ")
	return cmd[0], cmd[1:]
}
func getSafeArg(args []string, argIndex int) (string, error) {
	if argIndex < len(args) {
		return args[argIndex], nil
	}
	return "", ErrIndexOutOfRange
}

func writeOKResponse(conn net.Conn, msg string, log bool, args ...interface{}) {
	_, _ = fmt.Fprintf(conn, "+OK "+msg+eol, args...)
	if log {
		fmt.Printf("+OK "+msg, args...)
	}
}

func writeErrResponse(conn net.Conn, msg string, log bool, args ...interface{}) {
	_, _ = fmt.Fprintf(conn, "-ERR "+msg+eol, args...)
	if log {
		fmt.Printf("-ERR "+msg, args...)
	}
}

func deleteItems(emailDir string, mailData []*mailutils.MailData, deletedItems map[int]struct{}) (removeSucceed int, removeFailed int) {
	for id := range deletedItems {
		if id < 0 || id >= len(mailData) {
			removeFailed++
			continue
		}
		filename := filepath.Join(emailDir, mailData[id].Name)
		errJSON := os.Remove(filename + ".json")
		errFile := os.Remove(filename)

		if errJSON != nil {
			log.Printf("Warning: failed to remove %s.json: %v\n", filename, errJSON)
		}
		if errFile != nil {
			log.Printf("Warning: failed to remove %s: %v\n", filename, errFile)
		}

		if errJSON == nil && errFile == nil {
			removeSucceed++
		} else {
			removeFailed++
		}
	}
	return
}
