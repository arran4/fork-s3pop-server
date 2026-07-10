package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"testing"

	"github.com/FractalJim/s3pop-server/mailutils"
)

// setupSession creates a mock pop3Session and a net.Conn for testing
func setupSession(t *testing.T, state int) (*pop3Session, net.Conn, func()) {
	t.Helper()
	serverConn, clientConn := net.Pipe()

	var logBuf bytes.Buffer
	testLogger := log.New(&logBuf, "[TEST] ", log.Flags())

	sess := &pop3Session{
		conn:         serverConn,
		sessionLog:   testLogger,
		config:       &ServerConfig{},
		state:        state,
		deletedItems: make(map[int]struct{}),
	}

	cleanup := func() {
		serverConn.Close()
		clientConn.Close()
	}

	return sess, clientConn, cleanup
}

func readResponse(t *testing.T, conn net.Conn) string {
	t.Helper()
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read from conn: %v", err)
	}
	return string(buf[:n])
}

// readMultilineResponse reads from the connection until the multiline terminator ".\r\n" is found
func readMultilineResponse(t *testing.T, conn net.Conn) string {
	t.Helper()
	var result string
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Failed to read from conn: %v", err)
		}
		result += string(buf[:n])
		if len(result) >= 3 && result[len(result)-3:] == ".\r\n" {
			break
		}
	}
	return result
}

func TestConfigUnmarshal(t *testing.T) {
	jsonData := []byte(`{"s3Endpoint": "http://localhost:9000", "s3ForcePathStyle": true}`)
	config := new(ServerConfig)
	err := json.Unmarshal(jsonData, config)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	if config.S3Endpoint != "http://localhost:9000" {
		t.Errorf("Expected S3Endpoint to be 'http://localhost:9000', got '%v'", config.S3Endpoint)
	}

	forcePathStyle := (*bool)(config.S3ForcePathStyle)
	if forcePathStyle == nil || *forcePathStyle != true {
		t.Errorf("Expected S3ForcePathStyle to be true, got %v", config.S3ForcePathStyle)
	}
}

func TestHandleNOOP(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	go sess.handleNOOP([]string{})

	resp := readResponse(t, clientConn)
	if resp != "+OK \r\n" {
		t.Errorf("Expected '+OK \\r\\n', got %q", resp)
	}
}

func TestHandleRSET(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	sess.deletedItems[1] = struct{}{}
	go sess.handleRSET([]string{})

	resp := readResponse(t, clientConn)
	if resp != "+OK \r\n" {
		t.Errorf("Expected '+OK \\r\\n', got %q", resp)
	}
	if len(sess.deletedItems) != 0 {
		t.Errorf("Expected deletedItems to be empty, got %v", sess.deletedItems)
	}
}

func TestHandleSTAT(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	sess.mailData = []*mailutils.MailData{
		{TotalSize: 100},
		{TotalSize: 200},
	}

	go sess.handleSTAT([]string{})

	resp := readResponse(t, clientConn)
	if resp != "+OK 2 300\r\n" {
		t.Errorf("Expected '+OK 2 300\\r\\n', got %q", resp)
	}
}

func TestHandleDELE(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	sess.mailData = []*mailutils.MailData{
		{TotalSize: 100},
		{TotalSize: 200},
	}

	go sess.handleDELE([]string{"1"})

	resp := readResponse(t, clientConn)
	if resp != "+OK\r\n" {
		t.Errorf("Expected '+OK\\r\\n', got %q", resp)
	}
	if _, ok := sess.deletedItems[0]; !ok {
		t.Errorf("Expected item 0 to be marked as deleted")
	}

	go sess.handleDELE([]string{"3"}) // Out of range
	resp = readResponse(t, clientConn)
	if resp != "-ERR no such message\r\n" {
		t.Errorf("Expected '-ERR no such message\\r\\n', got %q", resp)
	}

	go sess.handleDELE([]string{"1"}) // Already deleted
	resp = readResponse(t, clientConn)
	if resp != "-ERR message already deleted\r\n" {
		t.Errorf("Expected '-ERR message already deleted\\r\\n', got %q", resp)
	}
}

func TestHandleLIST(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	sess.mailData = []*mailutils.MailData{
		{TotalSize: 100},
		{TotalSize: 200},
	}

	go sess.handleLIST([]string{})

	resp := readMultilineResponse(t, clientConn)
	expectedList := "+OK 2 messages (300 octets)\r\n1 100\r\n2 200\r\n.\r\n"
	if resp != expectedList {
		t.Errorf("Expected %q, got %q", expectedList, resp)
	}

	go sess.handleLIST([]string{"1"})
	resp = readResponse(t, clientConn)
	if resp != "+OK 1 100\r\n" {
		t.Errorf("Expected '+OK 1 100\\r\\n', got %q", resp)
	}

	go sess.handleLIST([]string{"3"})
	resp = readResponse(t, clientConn)
	if resp != "-ERR no such message\r\n" {
		t.Errorf("Expected '-ERR no such message\\r\\n', got %q", resp)
	}
}

func TestHandleUIDL(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	sess.mailData = []*mailutils.MailData{
		{Name: "msg1-1234"},
		{Name: "msg2-5678"},
	}

	go sess.handleUIDL([]string{})

	resp := readMultilineResponse(t, clientConn)
	expectedList := "+OK \r\n1 msg1-1234\r\n2 msg2-5678\r\n.\r\n"
	if resp != expectedList {
		t.Errorf("Expected %q, got %q", expectedList, resp)
	}

	go sess.handleUIDL([]string{"2"})
	resp = readResponse(t, clientConn)
	if resp != "+OK 2 msg2-5678\r\n" {
		t.Errorf("Expected '+OK 2 msg2-5678\\r\\n', got %q", resp)
	}
}

func TestHandleRETR(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	tmpDir := t.TempDir()
	sess.emailDir = tmpDir

	msgContent := "Subject: Test\r\n\r\nBody of message"
	err := os.WriteFile(tmpDir+"/test_msg.txt", []byte(msgContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock email: %v", err)
	}

	sess.mailData = []*mailutils.MailData{
		{Name: "test_msg.txt", TotalSize: len(msgContent)},
	}

	go sess.handleRETR([]string{"1"})

	resp := readMultilineResponse(t, clientConn)
	expectedList := fmt.Sprintf("+OK %d octets\r\nSubject: Test\r\n\r\nBody of message\r\n.\r\n", len(msgContent))
	if resp != expectedList {
		t.Errorf("Expected %q, got %q", expectedList, resp)
	}
}

func TestHandleTOP(t *testing.T) {
	sess, clientConn, cleanup := setupSession(t, stateTransaction)
	defer cleanup()

	tmpDir := t.TempDir()
	sess.emailDir = tmpDir

	msgContent := "Subject: Test\r\n\r\nBody line 1\r\nBody line 2\r\nBody line 3\r\n"
	err := os.WriteFile(tmpDir+"/test_msg_top.txt", []byte(msgContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock email: %v", err)
	}

	sess.mailData = []*mailutils.MailData{
		{Name: "test_msg_top.txt", TotalSize: len(msgContent)},
	}

	go sess.handleTOP([]string{"1", "1"})

	resp := readMultilineResponse(t, clientConn)
	expectedOutput := fmt.Sprintf("+OK %d octets\r\nSubject: Test\r\n\r\nBody line 1\r\n.\r\n", len(msgContent))

	if resp != expectedOutput {
		t.Errorf("Expected %q, got %q", expectedOutput, resp)
	}
}
