package main

import (
	"bufio"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func startTestServer(t *testing.T) *Server{
	t.Helper()
	srv := NewServer("127.0.0.1:0") // port 0 = OS picks a free port

	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *Server) net.Conn{
	t.Helper()
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {conn.Close()})
	return conn
}

// expectReply sends raw bytes, then reads exactly len(want) bytes back.
func expectReply(t *testing.T, conn net.Conn, send, want string) {
	t.Helper()
	if _, err := conn.Write([]byte(send)); err != nil {
		t.Fatal(err)
	}

	conn.SetDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reading reply: %v (got %q so far)", err, got)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPing(t *testing.T) {
	conn := dial(t, startTestServer(t))
	expectReply(t, conn, "*1\r\n$4\r\nPING\r\n", "+PONG\r\n")
}

func TestPingWithMessage(t *testing.T) {
	conn := dial(t, startTestServer(t))
	expectReply(t, conn, "*2\r\n$4\r\nPING\r\n$2\r\nhi\r\n", "$2\r\nhi\r\n")
}

func TestEcho(t *testing.T) {
	conn := dial(t, startTestServer(t))
	expectReply(t, conn, "*2\r\n$4\r\nECHO\r\n$5\r\nhello\r\n", "$5\r\nhello\r\n")
}

func TestCommandNamesAreCaseInsensitive(t *testing.T) {
	conn := dial(t, startTestServer(t))
	expectReply(t, conn, "*1\r\n$4\r\nping\r\n", "+PONG\r\n")
}

func TestUnknownCommand(t *testing.T) {
	conn := dial(t, startTestServer(t))
	expectReply(t, conn, "*1\r\n$3\r\nFOO\r\n", "-ERR unknown command 'FOO'\r\n")
}

func TestWrongNumberOfArguments(t *testing.T) {
	conn := dial(t, startTestServer(t))
	expectReply(t, conn, "*1\r\n$4\r\nECHO\r\n", "-ERR wrong number of arguments for 'echo' command\r\n")
}

// Several commands in one TCP write (pipelining), each gets its own reply
func TestPipelinedCommands(t *testing.T) {
	conn := dial(t, startTestServer(t))
	two := "*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\n"
	expectReply(t, conn, two, "+PONG\r\n+PONG\r\n")
}

// One command dribbled out a byte at a time must still be understood
func TestCommandSplitAcrossManyWrites(t *testing.T) {
	conn := dial(t, startTestServer(t))

	for _, b := range []byte("*1\r\n$4\r\nPING\r\n") {
		if _, err := conn.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len("+PONG\r\n"))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "+PONG\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestProtocolErrorGetsErrorReplyThenDisconnect(t *testing.T) {
	conn := dial(t, startTestServer(t))
	conn.Write([]byte("hello\r\n")) // not RESP

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(conn)

	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "-ERR Protocol error") {
		t.Fatalf("got %q, want an -ERR Protocol error reply", line)
	}
	if _, err := r.ReadString('\n'); err != io.EOF {
		t.Fatalf("expected the server to close the connection, got %v", err)
	}
}

func TestServerHandlesManyClientsConcurrently(t *testing.T) {
	srv := startTestServer(t)

	const clients = 50
	var wg sync.WaitGroup
	errs := make(chan string, clients)

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", srv.Addr())
			if err != nil {
				errs <- err.Error()
				return
			}
			defer conn.Close()
			conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))

			got := make([]byte, len("+PONG\r\n"))
			
			if _, err := io.ReadFull(conn, got); err != nil || string(got) != "+PONG\r\n" {
				errs <- "bad reply"
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}