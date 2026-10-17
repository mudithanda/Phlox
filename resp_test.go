package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func parse(input string) ([]string, error) {
	return ReadCommand(bufio.NewReader(strings.NewReader(input)))
}

func TestReadCommandValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"ping", "*1\r\n$4\r\nPING\r\n", []string{"PING"}},
		{"set", "*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nmudit\r\n", []string{"SET", "name", "mudit"}},
		{"empty argument", "*2\r\n$4\r\nECHO\r\n$0\r\n\r\n", []string{"ECHO", ""}},
		
		// The length prefix is what lets an argument contain \r\n safely.
		
		{"argument containing CRLF", "*2\r\n$4\r\nECHO\r\n$6\r\nab\r\ncd\r\n", []string{"ECHO", "ab\r\ncd"}},
		{"empty array", "*0\r\n", []string{}},
	}
	
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parse(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadCommandProtocolErrors(t *testing.T) {
	tests := []struct{ name, input string }{
		{"does not start with *", "+OK\r\n"},
		{"inline command", "PING\r\n"},
		{"non-numeric array length", "*x\r\n"},
		{"huge array length", "*99999999\r\n"},
		{"element is not a bulk string", "*1\r\n:5\r\n"},
		{"negative bulk length", "*1\r\n$-1\r\n"},
		{"huge bulk length", "*1\r\n$99999999999\r\n"},
		{"bulk not ending in CRLF", "*1\r\n$4\r\nPINGXX"},
		{"bare LF instead of CRLF", "*1\n$4\n"},
		{"header line too long", "*" + strings.Repeat("1", 5000) + "\r\n"},
	}

	for _, tc := range tests{
		t.Run(tc.name, func(t *testing.T) {
			_, err := parse(tc.input)
			var pe *ProtocolError
			if !errors.As(err, &pe) {
				t.Fatalf("got error %v, want a *ProtocalError", err)
			}
		})
	}
}

func TestReadCommandEOF(t *testing.T) {
	// Closed cleanly between commands: plain io.EOF
	if _, err := parse(""); err != io.EOF {
		t.Fatalf("empty input: got %v, want io.EOF", err)
	}

	// Closed in the middle of a command: io.ErrUnexpectedEOF
	for _, input := range []string{
		"*2\r\n$4\r\nPING\r\n", // second argument missing
		"*1\r\n$4\r\nPI",       // data cut short
		"*1\r\n$4",             // header cut short
	} {
		if _, err := parse(input); err != io.ErrUnexpectedEOF {
			t.Errorf("%q: got %v, want io.ErrUnexpectedEOF", input, err)
		}
	}
}

// TCP may deliver one byte at a time but the parser must still work
func TestReadCommandSurvivesOneByteReads(t *testing.T) {
	input := "*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nmudit\r\n"

	r := bufio.NewReader(iotest.OneByteReader(strings.NewReader(input)))
	got, err := ReadCommand(r)
	if err != nil {
		t.Fatal(err)
	}
	
	if want := []string{"SET", "name", "mudit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Two commands arriving glued together must come out as two commands.
func TestReadCommandTwoCommandsInOneBuffer(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("*1\r\n$4\r\nPING\r\n*2\r\n$4\r\nECHO\r\n$2\r\nhi\r\n"))

	first, err := ReadCommand(r)
	if err != nil || !reflect.DeepEqual(first, []string{"PING"}) {
		t.Fatalf("first: got %q, %v", first, err)
	}

	second, err := ReadCommand(r)
	if err != nil || !reflect.DeepEqual(second, []string{"ECHO", "hi"}) {
		t.Fatalf("second: got %q, %v", second, err)
	}

	if _, err := ReadCommand(r); err != io.EOF {
		t.Fatalf("third: got %v, want io.EOF", err)
	}
}

func TestWriterEncodesEachType(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.SimpleString("OK")
	w.Error("ERR boom")
	w.Integer(42)
	w.Integer(-7)
	w.Bulk("hello")
	w.Bulk("")
	w.Null()
	w.ArrayHeader(2)

	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	want := "+OK\r\n" +
		"-ERR boom\r\n" +
		":42\r\n" +
		":-7\r\n" +
		"$5\r\nhello\r\n" +
		"$0\r\n\r\n" +
		"$-1\r\n" +
		"*2\r\n"
	
		if buf.String() != want {
			t.Fatalf("got %q\nwant %q", buf.String(), want)
		}
}

func TestWriterBulkIsBinarySafe(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.Bulk("a\r\nb")
	w.Flush()

	if want := "$4\r\na\r\nb\r\n"; buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestWriterCannotBeUsedToInjectReplies(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.SimpleString("hi\r\n+INJECTED")
	w.Error("ERR x\r\n:1")
	w.Flush()

	if want := "+hi  +INJECTED\r\n-ERR x  :1\r\n"; buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}