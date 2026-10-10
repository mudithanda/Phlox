package main

import (
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

func NewServer(addr string) * Server {
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

// handleConn echoes every byte it receives back to the client.
// replace the echo with a RESP parser.
func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	log.Printf("client connected: %s", conn.RemoteAddr())

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			// io.EOF means the client closed the connection normally.
			log.Printf("client disconnected: %s", conn.RemoteAddr())
			return
		}
		log.Printf("received %d bytes: %q", n, buf[:n])
		if _, err := conn.Write(buf[:n]); err != nil {
			return
		}
	}
}

// Close stops accepting new connections.
func (s *Server) Close(){
	s.listener.Close()
}