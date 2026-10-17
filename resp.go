package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Limits -> protect server from clients that announce absured sizes
const (
	maxBulkLen = 64 * 1024 * 1024 // longest single to accept
	maxArrayLen = 1024 * 1024 // most arguments in one command
)

// ProtocolError means the client sent bytes that are not valid RESP
// After one, we cannot know where the next message starts, so connextion must be closed

type ProtocolError struct {Msg string}

func (e *ProtocolError) Error() string {
	return "Protocol Error: " + e.Msg
}

func protoErr(format string, a ...any) error {
	return &ProtocolError{
		Msg: fmt.Sprintf(format, a...),
	}
}

//-----------------------------------
// READING CLIENT COMMANDS (Parsing)
//-----------------------------------

// ReadCommand reads one command from r.clients and sends command as an
// array of bulk strings. Eg: SET name abcde is:

// *3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nabcde\r\n

// It returns io.EOF if the client closed the connection between commands,
// io.ErrUnexpectedEOF if it closed in the middle of one,
// *ProtocolError if the bytes are not valid RESP.

func ReadCommand(r *bufio.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err // io.EOF here is a normal disconnect
	}

	if len(line) == 0 || line[0] != '*' {
		return nil, protoErr("expected '*' at the satrt of command")
	}

	n, err := strconv.Atoi(line[1:]) 
	if err != nil || n > maxArrayLen {
		return nil, protoErr("invalid multibulk length")
	}

	if n <= 0 {
		return []string{}, nil // empty command = caller ignores it
	}

	capHint := n
	if capHint > 16 {
		capHint = 16 // don't trust client's number for allocation
	}

	args := make([]string, 0, capHint)

	for i := 0; i < n; i++ {
		arg, err := readBulk(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

// readLine reads up to and including "\n" and returns the line without the trailing "\r\n".
// The bufio buffer size (4096) caps the line length.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return "", protoErr("line too long")
		}
		if err == io.EOF && len(line) > 0 {
			err = io.ErrUnexpectedEOF // stream ended midline
		}
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", protoErr("Line must end with CRLF")
	}
	return string(line[:len(line)-2]), nil
}

// readBulk reads one bulk string: "$<len>\r\n<len bytes>\r\n"
func readBulk(r *bufio.Reader) (string, error) {
	line, err := readLine(r)
	if err != nil {
		return "", unexpectedEOF(err)
	}
	if len(line) == 0 || line[0] != '$' {
		return "", protoErr("Expected '$' before the argument")
	}

	n, err := strconv.Atoi(line[1:])
	if err != nil || n < 0 || n > maxBulkLen {
		return "", protoErr("invalid bulk length")
	}

	// Read exactly n bytes of data plus the closing \r\n. io.ReadFull keeps reading until it has them all
	// however the network chopped them up.
	
	buf := make([]byte, n+2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", unexpectedEOF(err)
	}
	if buf[n] != '\r' || buf[n+1] != '\n' {
		return "", protoErr("bulk string not terminated by CRLF")
	}
	return string(buf[:n]), nil 
}

// Once a command has started, running out of bytes is never "normal".
func unexpectedEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

//-----------------
// WRITING REPLIES
//-----------------

// Writer encodes RESP replies. It buffers, nothing reaches the client until Flush.
// bufio.Writer remembers the first write error, so the methods below don't return errors: check Flush instead.

type Writer struct {
	bw *bufio.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer {
		bw: bufio.NewWriter(w),
	}
}

// Simple strings and errors are single lines, so CR/LF inside them would let a caller inject extra replies. 
// Replace them with spaces.

var lineSafe = strings.NewReplacer("\r", " ", "\n", " ")

// SimpleString writes +OK\r\n style replies.
func (w *Writer) SimpleString(s string) {
	w.bw.WriteByte('+')
	w.bw.WriteString(lineSafe.Replace(s))
	w.bw.WriteString("\r\n")
}

// Error writes -ERR something\r\n. msg should start with an error kind such as "ERR".
func (w *Writer) Error(msg string) {
	w.bw.WriteByte('-')
	w.bw.WriteString(lineSafe.Replace(msg))
	w.bw.WriteString("\r\n")
}

// Integer writes :42\r\n.
func (w *Writer) Integer(n int64) {
	w.bw.WriteByte(':')
	w.bw.WriteString(strconv.FormatInt(n, 10))
	w.bw.WriteString("\r\n")
}

// Bulk writes $<len>\r\n<data>\r\n. Data may contain any bytes.
func (w *Writer) Bulk(s string) {
	w.bw.WriteByte('$')
	w.bw.WriteString(strconv.Itoa(len(s)))
	w.bw.WriteString("\r\n")
	w.bw.WriteString(s)
	w.bw.WriteString("\r\n")
}

// Null writes the "no such value" reply: $-1\r\n.
func(w *Writer) Null() {
	w.bw.WriteString("$-1\r\n")
}

// ArrayHeader writes *<n>\r\n; the caller then writes n elements.
func(w *Writer) ArrayHeader(n int) {
	w.bw.WriteByte('*')
	w.bw.WriteString(strconv.Itoa(n))
	w.bw.WriteString("\r\n")
}

// Flush sends everything buffered to the client.
func(w *Writer) Flush() error {
	return w.bw.Flush()
}

