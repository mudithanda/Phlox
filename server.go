package main

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"sync"
)

// server accepts TCP connections and handles each one in its own goroutine
type Server struct {
	addr     string
	listener net.Listener
	wg sync.WaitGroup
}

func NewServer(addr string) *Server {
	return &Server{
		addr: addr,
	}
}

// Start begins listening and returns immediately 
// connections are accepted in the background.

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln
	log.Printf("listening on %s", ln.Addr())

	s.wg.Add(1) // accept loop is now one tracked task.
	go s.acceptLoop()
	return nil
}

// Addr returns the real address (useful when addr was ":0").
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for{
		conn, err := s.listener.Accept()
		if err != nil {
			// Accept fails once Close() closes the listener -> time to stop.
			return
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn serves one client: read a command, run it, send the reply
// repeat until the client leaves or sends something invalid.
func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	remote := conn.RemoteAddr()
	log.Printf("client connected: %s", remote)
	defer log.Printf("client disconnected: %s", remote)

	// The bufio.Reader sits between the socket and the parser. It keeps
	// bytes that arrived early (the start of the next command) so they are not lost between ReadCommand calls.
	
	r := bufio.NewReader(conn)
	w := NewWriter(conn)

	for {
		args, err := ReadCommand(r)
		if err != nil {
			var pe *ProtocolError
			switch {
			case errors.As(err, &pe):
				// The stream is out of sync and cannot be recovered
				// tell the client why, then hang up.
				w.Error("ERR Protocol error: " + pe.Msg)
				w.Flush()
			case !errors.Is(err, io.EOF):
				log.Printf("client %s: %v", remote, err)
			}
			return
		}
		if len(args) == 0 {
			continue
		}
		execute(w, args)
		if err := w.Flush(); err != nil {
			return
		}
	}
}

// Close stops accepting new connections.
func (s *Server) Close(){
	s.listener.Close()
}