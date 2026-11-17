// SPDX-License-Identifier: GPL-3.0-only

package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netrave/internal/config"
)

func TestLockPreventsSecondInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	first, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := acquireLock(path); err == nil {
		t.Fatal("second lock must fail")
	}
}

func TestTailAndStatus(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("one\ntwo\nthree\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := tail(logPath, 2)
	if err != nil || data != "two\nthree" {
		t.Fatalf("tail: %q %v", data, err)
	}
	cfg := &config.Config{LogFile: logPath}
	s := &server{cfg: cfg, started: time.Now().Add(-time.Minute)}
	response := s.command(Request{Command: "status"})
	if !response.OK || response.State != "running" || response.Uptime < 60 {
		t.Fatalf("status: %+v", response)
	}
	logs := s.command(Request{Command: "logs", Lines: 1})
	if logs.Logs != "three" {
		t.Fatalf("logs: %+v", logs)
	}
}

func TestIPCCall(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		request := Request{}
		_ = json.NewDecoder(conn).Decode(&request)
		_ = json.NewEncoder(conn).Encode(Response{OK: true, State: "running", PID: 42, Version: "test"})
	}()
	response, err := Call(socket, Request{Command: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if response.PID != 42 || response.State != "running" {
		t.Fatalf("response: %+v", response)
	}
}

func TestCallRejectsServerError(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		request := Request{}
		_ = json.NewDecoder(conn).Decode(&request)
		_ = json.NewEncoder(conn).Encode(Response{Error: "denied"})
	}()
	if _, err := Call(socket, Request{Command: "stop"}); err == nil {
		t.Fatal("expected error")
	}
}
