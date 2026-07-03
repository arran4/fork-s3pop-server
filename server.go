// RFC https://tools.ietf.org/html/rfc1939
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
	"bufio"
	"context"
	"encoding/json"
	"fmt"

	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/FractalJim/s3pop-server/backend"
	"github.com/FractalJim/s3pop-server/mailutils"
)

const (
	stateUnauthorized = 1
	stateTransaction  = 2
	stateUpdate       = 3
)

const eol = "\r\n"
const multilineTerminator = ".\r\n"
const defaultport = 5110

type ServerConfig struct {
	Port             int                      `json:"port"`
	S3Bucket         string                   `json:"s3Bucket"`
	S3Endpoint       backend.S3Endpoint       `json:"s3Endpoint"`
	S3ForcePathStyle backend.S3ForcePathStyle `json:"s3ForcePathStyle"`
}

func main() {
	config := loadConfig()

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(config.Port))

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error.. %s", err.Error())
	}
	fmt.Println("Server started.")
	fmt.Println("Listening on port: " + strconv.Itoa(config.Port))
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		// run as goroutine
		go handleClient(conn, config)
	}

}

func loadConfig() (config *ServerConfig) {
	configFilename := os.Getenv("S3POP_CONFIG")
	if configFilename == "" {
		configFilename = "server-config.json"
	}

	config = new(ServerConfig)
	config.Port = defaultport
	jsonData, err := os.ReadFile(configFilename)
	if err == nil {
		err = json.Unmarshal(jsonData, config)
		if err != nil {
			log.Fatalf("Config file is not valid JSON: %v", err)
		}
	} else if !os.IsNotExist(err) {
		log.Fatalf("Error reading config file: %v", err)
	}

	if portStr := os.Getenv("S3POP_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 && p <= 65535 {
			config.Port = p
		} else {
			log.Fatalf("Invalid S3POP_PORT: %s (must be a valid port number between 1 and 65535)", portStr)
		}
	}
	if bucket := os.Getenv("S3POP_S3_BUCKET"); bucket != "" {
		config.S3Bucket = bucket
	}
	if endpoint := os.Getenv("S3POP_S3_ENDPOINT"); endpoint != "" {
		config.S3Endpoint = backend.S3Endpoint(endpoint)
	}
	if forcePathStyle := os.Getenv("S3POP_S3_FORCE_PATH_STYLE"); forcePathStyle != "" {
		b, err := strconv.ParseBool(forcePathStyle)
		if err != nil {
			log.Fatalf("Invalid S3POP_S3_FORCE_PATH_STYLE: %v", err)
		}
		config.S3ForcePathStyle = backend.S3ForcePathStyle(&b)
	}

	if config.S3Bucket == "" {
		log.Fatal("S3Bucket must be provided via config file or S3POP_S3_BUCKET environment variable")
	}

	return
}

func handleClient(conn net.Conn, config *ServerConfig) {
	defer func() { _ = conn.Close() }()

	var state = stateUnauthorized
	var emailDir string
	var emailBucket = config.S3Bucket
	var deletedItems map[int]struct{}
	var mailData []*mailutils.MailData
	reader := bufio.NewReader(conn)

	_, _ = fmt.Fprintf(conn, "+OK S3 POP3 server: powered by Go"+eol)

	for {
		// Reads a line from the client
		rawLine, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error!!" + err.Error())
			return
		}

		// Parses the command
		cmd, args := getCommand(rawLine)

		fmt.Println("Recieved Command: " + cmd)
		err = nil
		argNum := 0
		var arg string
		for err == nil {
			arg, err = getSafeArg(args, argNum)
			if nil == err {
				fmt.Printf("Argument %d: %s\n", argNum, arg)
			}
			argNum++
		}
		fmt.Println("")
		if cmd == "USER" && state == stateUnauthorized {
			//User name is name of folder in bucket in S3
			userName, err := getSafeArg(args, 0)
			if nil != err {
				writeErrResponse(conn, "No user name", false)
				continue
			}
			emailDir = mailutils.GetEmailDir(userName)
			err = backend.DownloadEmails(
				context.TODO(),
				emailBucket,
				userName,
				config.S3Endpoint,
				config.S3ForcePathStyle,
			)
			if nil != err {
				writeErrResponse(conn, "Could not download emails: %s", false, err)
				continue
			}
			mailData = getMessageData(emailDir)
			writeOKResponse(conn, "", true)

		} else if cmd == "PASS" && state == stateUnauthorized {
			//Accept all passwords (local servoce only)
			writeOKResponse(conn, "User signed in", true)
			deletedItems = make(map[int]struct{})
			state = stateTransaction

		} else if cmd == "STAT" && state == stateTransaction {
			count, size := getStat(mailData, deletedItems)
			writeOKResponse(conn, strconv.Itoa(count)+" "+strconv.Itoa(size), true)

		} else if cmd == "LIST" && state == stateTransaction {
			msgID, err := getSafeArg(args, 0)
			if err == nil {
				var id int
				id, _ = strconv.Atoi(msgID)
				id--
				if len(mailData) <= id {
					writeErrResponse(conn, "no such message", false)
					continue
				}
				if _, toDel := deletedItems[id]; toDel {
					writeErrResponse(conn, "message deleted", false)
					continue
				}
				writeOKResponse(conn, "%d %d", false, id+1, mailData[id].TotalSize)
			} else {
				count, size := getStat(mailData, deletedItems)
				writeOKResponse(conn, "%d messages (%d octets)", false, count, size)

				for itemID, mailItem := range mailData {
					if _, toDel := deletedItems[itemID]; toDel {
						continue
					}
					_, _ = fmt.Fprintf(conn, "%d %d\r\n", itemID+1, mailItem.TotalSize)
				}
				_, _ = fmt.Fprint(conn, multilineTerminator)
			}

		} else if cmd == "UIDL" && state == stateTransaction {

			msgID, err := getSafeArg(args, 0)
			var id int

			if err == nil {
				id, _ = strconv.Atoi(msgID)
				id--
				if len(mailData) <= id {
					writeErrResponse(conn, "no such message", false)
					continue
				}
				if _, toDel := deletedItems[id]; toDel {
					writeErrResponse(conn, "message deleted", false)
					continue
				}
				writeOKResponse(conn, "%d %s", false, id+1, mailData[id].Name)
			} else {
				writeOKResponse(conn, "", false)

				for id, mailItem := range mailData {
					if _, toDel := deletedItems[id]; toDel {
						continue
					}
					_, _ = fmt.Fprintf(conn, "%d %s\r\n", id+1, mailItem.Name)
				}
				_, _ = fmt.Fprint(conn, multilineTerminator)
			}

		} else if cmd == "TOP" && state == stateTransaction {
			msgID, err := getSafeArg(args, 0)
			var id int

			if err == nil {
				id, _ = strconv.Atoi(msgID)
				id--
				if len(mailData) <= id {
					writeErrResponse(conn, "no such message", false)
					continue
				}
				if _, toDel := deletedItems[id]; toDel {
					writeErrResponse(conn, "message deleted", false)
					continue
				}
			} else {
				writeErrResponse(conn, "no message selected", false)
				continue
			}
			lineArg, err := getSafeArg(args, 1)
			var lines int
			if nil != err {
				writeErrResponse(conn, "no line argument supplied", false)
				continue
			}
			lines, _ = strconv.Atoi(lineArg)

			fullFilePath := filepath.Join(emailDir, mailData[id].Name)
			fileData, err := os.Open(fullFilePath)
			if err != nil {
				writeErrResponse(conn, "failed to open email %s", false, mailData[id].Name)
			}
			defer func() { _ = fileData.Close() }()
			writeOKResponse(conn, "%d octets", false, mailData[id].TotalSize)
			bodyLinesRead := 0
			inBody := false
			fileScanner := bufio.NewScanner(fileData)
			for fileScanner.Scan() {
				line := fileScanner.Text()
				if line == "" && !inBody {
					_, _ = fmt.Fprint(conn, line+eol)
					inBody = true
				} else if line == "." {
					_, _ = fmt.Fprint(conn, eol+line+eol)
				} else {
					if inBody {
						bodyLinesRead++
						if bodyLinesRead > lines {
							break
						}
					}
					_, _ = fmt.Fprint(conn, line+eol)
				}

			}
			_, _ = fmt.Fprint(conn, multilineTerminator)
			_ = fileData.Close()

		} else if cmd == "RETR" && state == stateTransaction {
			msgID, err := getSafeArg(args, 0)
			var id int
			if err == nil {
				id, _ = strconv.Atoi(msgID)
				id--
				if len(mailData) <= id {
					writeErrResponse(conn, "no such message", false)
					continue
				}
				if _, toDel := deletedItems[id]; toDel {
					writeErrResponse(conn, "message deleted", false)
					continue
				}
			} else {
				writeErrResponse(conn, "no message selected", false)
				continue
			}

			fullFilePath := filepath.Join(emailDir, mailData[id].Name)
			fileData, err := os.Open(fullFilePath)
			if err != nil {
				writeErrResponse(conn, "failed to open email %s", false, mailData[id].Name)
			}
			defer func() { _ = fileData.Close() }()
			writeOKResponse(conn, "%d octets", false, mailData[id].TotalSize)

			fileScanner := bufio.NewScanner(fileData)
			for fileScanner.Scan() {
				line := fileScanner.Text()
				if line == "." {
					_, _ = fmt.Fprint(conn, eol+line+eol)
				} else {
					_, _ = fmt.Fprint(conn, line+eol)
				}

			}
			_, _ = fmt.Fprint(conn, multilineTerminator)
			_ = fileData.Close()

		} else if cmd == "DELE" && state == stateTransaction {
			msgID, err := getSafeArg(args, 0)
			var id int
			if err == nil {
				id, _ = strconv.Atoi(msgID)
				id--
				if len(mailData) <= id {
					writeErrResponse(conn, "no such message", false)
					continue
				}
				if _, toDel := deletedItems[id]; toDel {
					writeErrResponse(conn, "message already deleted", false)
					continue
				}
			} else {
				writeErrResponse(conn, "no message selected", false)
				continue
			}
			deletedItems[id] = struct{}{}
			_, _ = fmt.Fprintf(conn, "+OK"+eol)
		} else if cmd == "RSET" {
			deletedItems = make(map[int]struct{})
			writeOKResponse(conn, "", false)
		} else if cmd == "NOOP" {
			writeOKResponse(conn, "", false)
		} else if cmd == "QUIT" {
			if state == stateTransaction {
				_ = state
				deleteItems(emailDir, mailData, deletedItems)
			}
			return
		} else {
			writeErrResponse(conn, "Unrecognised Command", true)
		}
	}
}
