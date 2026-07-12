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
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

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
	ShutdownTimeout  int                      `json:"shutdownTimeout"` // in seconds
}

type pop3Session struct {
	conn         net.Conn
	sessionLog   *log.Logger
	config       *ServerConfig
	state        int
	emailDir     string
	emailBucket  string
	deletedItems map[int]struct{}
	mailData     []*mailutils.MailData
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

//go:embed usage.tmpl
var usageTmpl string

var sessionCounter uint64

func generateSessionID() string {
	id := atomic.AddUint64(&sessionCounter, 1)
	return fmt.Sprintf("%08x", id)
}

func main() {
	configFlag := flag.String("config", "", "Path to the configuration file")
	portFlag := flag.Int("port", 0, "Port to listen on (overrides config file and environment variables)")

	flag.Usage = func() {
		fmt.Print(usageTmpl)
	}

	flag.Parse()

	log.Printf("Starting S3 POP3 Server version: %s, commit: %s, date: %s", version, commit, date)

	config := loadConfig(configFlag, portFlag)

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(config.Port))

	if err != nil {
		log.Fatalf("Error.. %s", err.Error())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("Server started.")
	log.Println("Listening on port: " + strconv.Itoa(config.Port))

	var wg sync.WaitGroup

	go func() {
		<-ctx.Done()
		log.Println("Shutdown signal received. Shutting down gracefully...")
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			log.Printf("Accept error: %v", err)
			continue
		}
		// run as goroutine
		wg.Add(1)
		go func(c net.Conn, cfg *ServerConfig) {
			defer wg.Done()
			handleClient(c, cfg)
		}(conn, config)
	}

	log.Println("Waiting for active connections to finish...")
	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(time.Duration(config.ShutdownTimeout) * time.Second):
		log.Println("Shutdown timed out. Forcing exit.")
	}
	log.Println("Server stopped.")
}

func loadConfig(configFlag *string, portFlag *int) (config *ServerConfig) {
	log.Println("Discovering configuration...")

	configFilename := ""
	configExplicitlyRequested := false

	if configFlag != nil && *configFlag != "" {
		configFilename = *configFlag
		configExplicitlyRequested = true
		log.Printf("Using configuration file from command-line flag: %s", configFilename)
	} else if envConfig := os.Getenv("S3POP_CONFIG"); envConfig != "" {
		configFilename = envConfig
		configExplicitlyRequested = true
		log.Printf("Using configuration file from S3POP_CONFIG environment variable: %s", configFilename)
	} else {
		configFilename = "server-config.json"
	}

	config = new(ServerConfig)
	config.Port = defaultport
	config.ShutdownTimeout = 30 // default 30 seconds

	jsonData, err := os.ReadFile(configFilename)
	if err == nil {
		log.Printf("Successfully loaded configuration from %s", configFilename)
		err = json.Unmarshal(jsonData, config)
		if err != nil {
			log.Fatalf("Config file is not valid JSON: %v", err)
		}
	} else {
		if os.IsNotExist(err) {
			if configExplicitlyRequested {
				log.Fatalf("Specified config file %s does not exist", configFilename)
			} else {
				log.Printf("Default config file %s not found, continuing with environment variables", configFilename)
			}
		} else {
			if configExplicitlyRequested {
				log.Fatalf("Error reading explicitly requested config file: %v", err)
			} else {
				log.Printf("Error reading default config file: %v, continuing with environment variables", err)
			}
		}
	}

	if portFlag != nil && *portFlag != 0 {
		log.Printf("Using port from command-line flag: %d", *portFlag)
		config.Port = *portFlag
	} else if portStr := os.Getenv("S3POP_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 && p <= 65535 {
			log.Printf("Using S3POP_PORT from environment: %d", p)
			config.Port = p
		} else {
			log.Fatalf("Invalid S3POP_PORT: %s (must be a valid port number between 1 and 65535)", portStr)
		}
	}
	if bucket := os.Getenv("S3POP_S3_BUCKET"); bucket != "" {
		log.Printf("Using S3POP_S3_BUCKET from environment: %s", bucket)
		config.S3Bucket = bucket
	}
	if endpoint := os.Getenv("S3POP_S3_ENDPOINT"); endpoint != "" {
		log.Printf("Using S3POP_S3_ENDPOINT from environment: %s", endpoint)
		config.S3Endpoint = backend.S3Endpoint(endpoint)
	}
	if forcePathStyle := os.Getenv("S3POP_S3_FORCE_PATH_STYLE"); forcePathStyle != "" {
		log.Printf("Using S3POP_S3_FORCE_PATH_STYLE from environment: %s", forcePathStyle)
		b, err := strconv.ParseBool(forcePathStyle)
		if err != nil {
			log.Fatalf("Invalid S3POP_S3_FORCE_PATH_STYLE: %v", err)
		}
		config.S3ForcePathStyle = backend.S3ForcePathStyle(&b)
	}
	if timeout := os.Getenv("S3POP_SHUTDOWN_TIMEOUT"); timeout != "" {
		if t, err := strconv.Atoi(timeout); err == nil && t > 0 {
			log.Printf("Using S3POP_SHUTDOWN_TIMEOUT from environment: %d", t)
			config.ShutdownTimeout = t
		} else {
			log.Fatalf("Invalid S3POP_SHUTDOWN_TIMEOUT: %s (must be a positive integer in seconds)", timeout)
		}
	}

	if config.Port <= 0 || config.Port > 65535 {
		log.Fatalf("Invalid port: %d (must be a valid port number between 1 and 65535)", config.Port)
	}

	if config.S3Bucket == "" {
		log.Fatal("S3Bucket must be provided via config file or S3POP_S3_BUCKET environment variable. Valid configuration is required at startup.")
	}

	return
}

func handleClient(conn net.Conn, config *ServerConfig) {
	sessionID := generateSessionID()
	sessionLog := log.New(log.Writer(), fmt.Sprintf("[%s] ", sessionID), log.Flags())

	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			sessionLog.Printf("Error closing connection: %v\n", err)
		}
	}()

	sess := &pop3Session{
		conn:        conn,
		sessionLog:  sessionLog,
		config:      config,
		state:       stateUnauthorized,
		emailBucket: config.S3Bucket,
	}

	reader := bufio.NewReader(conn)

	_, _ = fmt.Fprintf(conn, "+OK S3 POP3 server: powered by Go"+eol)

	for {
		// Reads a line from the client
		rawLine, err := reader.ReadString('\n')
		if err != nil {
			sessionLog.Printf("Error reading from client: %v", err)
			return
		}

		// Parses the command
		cmd, args := getCommand(rawLine)

		sessionLog.Printf("Received Command: %s", cmd)
		for i := 0; ; i++ {
			arg, err := getSafeArg(args, i)
			if err != nil {
				break
			}
			sessionLog.Printf("Argument %d: %s", i, arg)
		}

		switch cmd {
		case "USER":
			sess.handleUSER(args)
		case "PASS":
			sess.handlePASS(args)
		case "STAT":
			sess.handleSTAT(args)
		case "LIST":
			sess.handleLIST(args)
		case "UIDL":
			sess.handleUIDL(args)
		case "TOP":
			sess.handleTOP(args)
		case "RETR":
			sess.handleRETR(args)
		case "DELE":
			sess.handleDELE(args)
		case "RSET":
			sess.handleRSET(args)
		case "NOOP":
			sess.handleNOOP(args)
		case "QUIT":
			if sess.handleQUIT(args) {
				return
			}
		default:
			writeErrResponse(conn, "Unrecognised Command", true)
		}
	}
}

func (s *pop3Session) handleUSER(args []string) {
	if s.state != stateUnauthorized {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	userName, err := getSafeArg(args, 0)
	if err != nil {
		writeErrResponse(s.conn, "No user name", false)
		return
	}
	s.emailDir, err = mailutils.GetEmailDir(userName)
	if err != nil {
		s.sessionLog.Printf("Error getting email directory: %v", err)
		writeErrResponse(s.conn, "Could not access user directory", false)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	err = backend.DownloadEmails(
		ctx,
		s.emailBucket,
		userName,
		s.config.S3Endpoint,
		s.config.S3ForcePathStyle,
	)
	if err != nil {
		writeErrResponse(s.conn, "Could not download emails: %s", false, err)
		return
	}
	s.mailData, err = getMessageData(s.emailDir)
	if err != nil {
		s.sessionLog.Printf("Error getting message data: %v", err)
		writeErrResponse(s.conn, "Could not access message data", false)
		return
	}
	writeOKResponse(s.conn, "", true)
}

func (s *pop3Session) handlePASS(args []string) {
	if s.state != stateUnauthorized {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	writeOKResponse(s.conn, "User signed in", true)
	s.deletedItems = make(map[int]struct{})
	s.state = stateTransaction
}

func (s *pop3Session) handleSTAT(args []string) {
	if s.state != stateTransaction {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	count, size := getStat(s.mailData, s.deletedItems)
	writeOKResponse(s.conn, strconv.Itoa(count)+" "+strconv.Itoa(size), true)
}

func (s *pop3Session) handleLIST(args []string) {
	if s.state != stateTransaction {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	msgID, err := getSafeArg(args, 0)
	if err == nil {
		id, _ := strconv.Atoi(msgID)
		id--
		if id < 0 || len(s.mailData) <= id {
			writeErrResponse(s.conn, "no such message", false)
			return
		}
		if _, toDel := s.deletedItems[id]; toDel {
			writeErrResponse(s.conn, "message deleted", false)
			return
		}
		writeOKResponse(s.conn, "%d %d", false, id+1, s.mailData[id].TotalSize)
	} else {
		count, size := getStat(s.mailData, s.deletedItems)
		writeOKResponse(s.conn, "%d messages (%d octets)", false, count, size)

		for itemID, mailItem := range s.mailData {
			if _, toDel := s.deletedItems[itemID]; toDel {
				continue
			}
			_, _ = fmt.Fprintf(s.conn, "%d %d\r\n", itemID+1, mailItem.TotalSize)
		}
		_, _ = fmt.Fprint(s.conn, multilineTerminator)
	}
}

func (s *pop3Session) handleUIDL(args []string) {
	if s.state != stateTransaction {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	msgID, err := getSafeArg(args, 0)
	if err == nil {
		id, _ := strconv.Atoi(msgID)
		id--
		if id < 0 || len(s.mailData) <= id {
			writeErrResponse(s.conn, "no such message", false)
			return
		}
		if _, toDel := s.deletedItems[id]; toDel {
			writeErrResponse(s.conn, "message deleted", false)
			return
		}
		writeOKResponse(s.conn, "%d %s", false, id+1, s.mailData[id].Name)
	} else {
		writeOKResponse(s.conn, "", false)

		for id, mailItem := range s.mailData {
			if _, toDel := s.deletedItems[id]; toDel {
				continue
			}
			_, _ = fmt.Fprintf(s.conn, "%d %s\r\n", id+1, mailItem.Name)
		}
		_, _ = fmt.Fprint(s.conn, multilineTerminator)
	}
}

func (s *pop3Session) handleTOP(args []string) {
	if s.state != stateTransaction {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	msgID, err := getSafeArg(args, 0)
	if err != nil {
		writeErrResponse(s.conn, "no message selected", false)
		return
	}
	id, _ := strconv.Atoi(msgID)
	id--
	if id < 0 || len(s.mailData) <= id {
		writeErrResponse(s.conn, "no such message", false)
		return
	}
	if _, toDel := s.deletedItems[id]; toDel {
		writeErrResponse(s.conn, "message deleted", false)
		return
	}

	lineArg, err := getSafeArg(args, 1)
	if err != nil {
		writeErrResponse(s.conn, "no line argument supplied", false)
		return
	}
	lines, err := strconv.Atoi(lineArg)
	if err != nil || lines < 0 {
		writeErrResponse(s.conn, "invalid line count", false)
		return
	}

	fullFilePath := filepath.Join(s.emailDir, s.mailData[id].Name)
	fileData, err := os.Open(fullFilePath)
	if err != nil {
		writeErrResponse(s.conn, "failed to open email %s", false, s.mailData[id].Name)
		return
	}
	defer fileData.Close()
	writeOKResponse(s.conn, "%d octets", false, s.mailData[id].TotalSize)
	bodyLinesRead := 0
	inBody := false
	fileScanner := bufio.NewScanner(fileData)
	for fileScanner.Scan() {
		line := fileScanner.Text()
		if line == "" && !inBody {
			_, _ = fmt.Fprint(s.conn, line+eol)
			inBody = true
		} else if line == "." {
			_, _ = fmt.Fprint(s.conn, eol+line+eol)
		} else {
			if inBody {
				bodyLinesRead++
				if bodyLinesRead > lines {
					break
				}
			}
			_, _ = fmt.Fprint(s.conn, line+eol)
		}
	}
	if err := fileScanner.Err(); err != nil {
		s.sessionLog.Printf("Error reading email file: %v", err)
	}
	_, _ = fmt.Fprint(s.conn, multilineTerminator)
}

func (s *pop3Session) handleRETR(args []string) {
	if s.state != stateTransaction {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	msgID, err := getSafeArg(args, 0)
	if err != nil {
		writeErrResponse(s.conn, "no message selected", false)
		return
	}
	id, _ := strconv.Atoi(msgID)
	id--
	if id < 0 || len(s.mailData) <= id {
		writeErrResponse(s.conn, "no such message", false)
		return
	}
	if _, toDel := s.deletedItems[id]; toDel {
		writeErrResponse(s.conn, "message deleted", false)
		return
	}

	fullFilePath := filepath.Join(s.emailDir, s.mailData[id].Name)
	fileData, err := os.Open(fullFilePath)
	if err != nil {
		writeErrResponse(s.conn, "failed to open email %s", false, s.mailData[id].Name)
		return
	}
	defer fileData.Close()
	writeOKResponse(s.conn, "%d octets", false, s.mailData[id].TotalSize)

	fileScanner := bufio.NewScanner(fileData)
	for fileScanner.Scan() {
		line := fileScanner.Text()
		if line == "." {
			_, _ = fmt.Fprint(s.conn, eol+line+eol)
		} else {
			_, _ = fmt.Fprint(s.conn, line+eol)
		}
	}
	if err := fileScanner.Err(); err != nil {
		s.sessionLog.Printf("Error reading email file: %v", err)
	}
	_, _ = fmt.Fprint(s.conn, multilineTerminator)
}

func (s *pop3Session) handleDELE(args []string) {
	if s.state != stateTransaction {
		writeErrResponse(s.conn, "Command not valid in this state", false)
		return
	}
	msgID, err := getSafeArg(args, 0)
	if err != nil {
		writeErrResponse(s.conn, "no message selected", false)
		return
	}
	id, _ := strconv.Atoi(msgID)
	id--
	if id < 0 || len(s.mailData) <= id {
		writeErrResponse(s.conn, "no such message", false)
		return
	}
	if _, toDel := s.deletedItems[id]; toDel {
		writeErrResponse(s.conn, "message already deleted", false)
		return
	}
	s.deletedItems[id] = struct{}{}
	_, _ = fmt.Fprintf(s.conn, "+OK"+eol)
}

func (s *pop3Session) handleRSET(args []string) {
	s.deletedItems = make(map[int]struct{})
	writeOKResponse(s.conn, "", false)
}

func (s *pop3Session) handleNOOP(args []string) {
	writeOKResponse(s.conn, "", false)
}

func (s *pop3Session) handleQUIT(args []string) bool {
	if s.state == stateTransaction {
		deleteItems(s.emailDir, s.mailData, s.deletedItems)
	}
	return true // Signal to close connection
}
