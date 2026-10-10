package main

import (
	"bufio"
	"net"
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

func TestServerEchoesData(t *testing.T) {
	srv := startTestServer(t)

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	if line != "hello\n" {
		t.Fatalf("got %q, want %q", line, "hello\n")
	}
}

func TestServerHandlesManyClientsConcurrently(t *testing.T) {
	srv := startTestServer(t)

	const clients = 50
	var wg sync.WaitGroup
	errs := make(chan error, clients)

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", srv.Addr())
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			conn.Write([]byte("ping\n"))
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, err := bufio.NewReader(conn).ReadString('\n')

			if err != nil || line != "ping\n" {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}