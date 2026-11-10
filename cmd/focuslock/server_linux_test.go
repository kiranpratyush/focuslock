//go:build linux

package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFrameServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.sock")
	srv, err := listenFrames(path)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("socket open to others: %v %v", fi.Mode(), err)
	}

	srv.send(wireFrame{Type: "frame", Bar: "first"})
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	read := func() wireFrame {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var f wireFrame
		if err := json.Unmarshal(line, &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	// A new client gets the last frame at once, then each new one.
	if f := read(); f.Bar != "first" {
		t.Fatalf("got %q", f.Bar)
	}
	srv.send(wireFrame{Type: "frame", Bar: "second"})
	if f := read(); f.Bar != "second" {
		t.Fatalf("got %q", f.Bar)
	}

	_, _ = c.Write([]byte(`{"type":"explain","reason":"need the API docs"}` + "\n" + "not json\n"))
	select {
	case cmd := <-srv.commands:
		if cmd.Type != "explain" || cmd.Reason != "need the API docs" {
			t.Fatalf("command = %+v", cmd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command")
	}

	// Typing stops when the app goes away.
	_, _ = c.Write([]byte(`{"type":"typing","on":true}` + "\n"))
	if cmd := <-srv.commands; !cmd.On {
		t.Fatalf("command = %+v", cmd)
	}
	c.Close()
	select {
	case cmd := <-srv.commands:
		if cmd.Type != "typing" || cmd.On {
			t.Fatalf("command = %+v", cmd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("typing not reset on disconnect")
	}
}
