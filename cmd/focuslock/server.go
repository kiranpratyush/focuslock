//go:build windows || linux

package main

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"

	"focuslock/internal/logx"
)

// The worker's socket: frames go out to the Qt app as JSON lines, and
// commands come back the same way. Commands are handed to the loop on a
// channel, because the engine isn't safe for concurrent use.

// command is one line from a frontend.
type command struct {
	Type   string `json:"type"`             // "explain", "typing"
	Reason string `json:"reason,omitempty"` // explain
	On     bool   `json:"on,omitempty"`     // typing: the explanation box has focus
}

type frameServer struct {
	ln       net.Listener
	commands chan command

	mu      sync.Mutex
	clients map[net.Conn]bool
	last    []byte
}

// listenFrames serves addr, a socket (Linux) or named pipe (Windows) only
// this user can open.
func listenFrames(addr string) (*frameServer, error) {
	ln, err := listenLocal(addr)
	if err != nil {
		return nil, err
	}
	s := &frameServer{ln: ln, commands: make(chan command, 16), clients: map[net.Conn]bool{}}
	go s.accept()
	return s, nil
}

func (s *frameServer) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.clients[c] = true
		last := s.last
		s.mu.Unlock()
		if last != nil {
			_, _ = c.Write(last) // a new client draws at once
		}
		go s.read(c)
	}
}

func (s *frameServer) read(c net.Conn) {
	defer s.drop(c)
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 4096), 64*1024)
	for sc.Scan() {
		var cmd command
		if json.Unmarshal(sc.Bytes(), &cmd) != nil {
			continue
		}
		select {
		case s.commands <- cmd:
		default:
			logx.Printf("dropped a %q command: the loop is behind", cmd.Type)
		}
	}
}

func (s *frameServer) drop(c net.Conn) {
	s.mu.Lock()
	_, was := s.clients[c]
	delete(s.clients, c)
	s.mu.Unlock()
	_ = c.Close()
	if was {
		// An app that's gone can't be typing: don't leave the countdown paused.
		select {
		case s.commands <- command{Type: "typing", On: false}:
		default:
		}
	}
}

// send writes v as one line to every client.
func (s *frameServer) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	s.mu.Lock()
	s.last = b
	clients := make([]net.Conn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		if _, err := c.Write(b); err != nil {
			s.drop(c)
		}
	}
}

func (s *frameServer) close() {
	_ = s.ln.Close()
	s.mu.Lock()
	for c := range s.clients {
		_ = c.Close()
	}
	s.mu.Unlock()
}
