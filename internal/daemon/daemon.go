// SPDX-License-Identifier: GPL-3.0-only

// Package daemon supervises the HTTP server, exposes a Unix-socket control
// channel, enforces one instance per host and restarts the child on failure.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"netrave/internal/config"
	"netrave/internal/version"
)

// Request is a single IPC command. The protocol is one JSON object per
// connection, terminated by EOF.
type Request struct {
	Command string `json:"command"`
	Lines   int    `json:"lines,omitempty"`
}

// Response is the IPC response.
type Response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	State    string `json:"state,omitempty"`
	PID      int    `json:"pid,omitempty"`
	ChildPID int    `json:"child_pid,omitempty"`
	Uptime   int64  `json:"uptime_sec,omitempty"`
	Restarts int    `json:"restarts,omitempty"`
	Version  string `json:"version,omitempty"`
	Logs     string `json:"logs,omitempty"`
}

type server struct {
	cfg      *config.Config
	logger   *slog.Logger
	logFile  *os.File
	lockFile *os.File
	listener net.Listener
	child    *exec.Cmd
	childMu  sync.Mutex
	started  time.Time
	restarts int
	shutdown context.CancelFunc
}

// Run starts the supervisor and blocks until SIGTERM/SIGINT or an IPC stop.
func Run(parent context.Context, cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	lock, err := acquireLock(filepath.Join(cfg.RunDir, "daemon.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	pidPath := filepath.Join(cfg.RunDir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o640); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidPath)

	logFile, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()
	writer := io.MultiWriter(logFile, os.Stderr)
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s := &server{cfg: cfg, logger: logger, logFile: logFile, lockFile: lock, started: time.Now(), shutdown: cancel}
	if err := s.listen(); err != nil {
		return err
	}
	defer s.closeListener()
	logger.Info("daemon started", "version", version.Version, "pid", os.Getpid(), "socket", cfg.SocketPath)

	go s.serveIPC()
	err = s.supervise(ctx)
	logger.Info("daemon stopped", "restarts", s.restarts)
	return err
}

func acquireLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another netrave daemon is already running")
	}
	return file, nil
}

func (s *server) listen() error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o750); err != nil {
		return err
	}
	_ = os.Remove(s.cfg.SocketPath)
	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen IPC socket: %w", err)
	}
	if err := os.Chmod(s.cfg.SocketPath, 0o660); err != nil {
		listener.Close()
		return err
	}
	s.listener = listener
	return nil
}

func (s *server) closeListener() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.cfg.SocketPath)
}

func (s *server) supervise(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		child, err := s.startChild()
		if err != nil {
			s.logger.Error("start child failed", "error", err)
			if !sleepContext(ctx, time.Duration(s.cfg.RestartSec)*time.Second) {
				return nil
			}
			s.restarts++
			continue
		}
		s.childMu.Lock()
		s.child = child
		s.childMu.Unlock()
		s.logger.Info("child started", "pid", child.Process.Pid)
		wait := make(chan error, 1)
		go func() { wait <- child.Wait() }()
		select {
		case <-ctx.Done():
			terminate(child)
			select {
			case <-wait:
			case <-time.After(90 * time.Second):
				kill(child)
				<-wait
			}
			s.childMu.Lock()
			s.child = nil
			s.childMu.Unlock()
			return nil
		case err = <-wait:
			s.childMu.Lock()
			s.child = nil
			s.childMu.Unlock()
			s.restarts++
			if err != nil {
				s.logger.Warn("child exited", "error", err, "restarts", s.restarts)
			} else {
				s.logger.Warn("child exited", "restarts", s.restarts)
			}
			if !sleepContext(ctx, time.Duration(s.cfg.RestartSec)*time.Second) {
				return nil
			}
		}
	}
}

func (s *server) startChild() (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"serve"}
	if s.cfg.ConfigPath != "" {
		args = append(args, "--config", s.cfg.ConfigPath)
	} else {
		args = append(args,
			"--addr", s.cfg.Addr,
			"--data", s.cfg.DataDir,
			"--disks", s.cfg.DisksDir,
			"--labs", s.cfg.LabsDir,
			"--instances", s.cfg.InstancesDir,
			"--logs", s.cfg.LogsDir,
			"--icons", s.cfg.IconsDir,
			"--db", s.cfg.DBPath,
			"--qemu", s.cfg.QEMUPath,
		)
		if s.cfg.StopVMsOnExit {
			args = append(args, "--stop-vms-on-exit")
		}
	}
	cmd := exec.Command(executable, args...)
	cmd.Stdout = s.logFile
	cmd.Stderr = s.logFile
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func terminate(child *exec.Cmd) {
	if child == nil || child.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(child.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = child.Process.Signal(syscall.SIGTERM)
	}
}

func kill(child *exec.Cmd) {
	if child == nil || child.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(child.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		_ = child.Process.Kill()
	}
}

func (s *server) serveIPC() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if !peerAllowed(conn) {
		_ = json.NewEncoder(conn).Encode(Response{Error: "permission denied"})
		return
	}
	request := Request{}
	decoder := json.NewDecoder(io.LimitReader(conn, 64*1024))
	if err := decoder.Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "invalid request"})
		return
	}
	response := s.command(request)
	_ = json.NewEncoder(conn).Encode(response)
	if request.Command == "stop" && response.OK {
		s.shutdown()
	}
}

func (s *server) command(request Request) Response {
	switch request.Command {
	case "ping":
		return Response{OK: true, Version: version.Version}
	case "status":
		s.childMu.Lock()
		childPID := 0
		if s.child != nil && s.child.Process != nil {
			childPID = s.child.Process.Pid
		}
		s.childMu.Unlock()
		return Response{OK: true, State: "running", PID: os.Getpid(), ChildPID: childPID,
			Uptime: int64(time.Since(s.started).Seconds()), Restarts: s.restarts, Version: version.Version}
	case "logs":
		logs, err := tail(s.cfg.LogFile, request.Lines)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Logs: logs}
	case "restart":
		s.childMu.Lock()
		child := s.child
		s.childMu.Unlock()
		if child != nil && child.Process != nil {
			pgid, _ := syscall.Getpgid(child.Process.Pid)
			if pgid > 0 {
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			} else {
				_ = child.Process.Signal(syscall.SIGTERM)
			}
		}
		return Response{OK: true, State: "restarting"}
	case "stop":
		return Response{OK: true, State: "stopping"}
	default:
		return Response{Error: "unknown command"}
	}
}

// Call sends one IPC command and returns the daemon response.
func Call(socket string, request Request) (Response, error) {
	conn, err := net.DialTimeout("unix", socket, 3*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, err
	}
	response := Response{}
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	return response, nil
}

func tail(path string, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n"), nil
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Background re-executes the daemon detached from the terminal. It is only
// used by `netrave daemon --background`; service managers use --foreground.
func Background(cfg *config.Config) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"daemon", "--foreground"}
	if cfg.ConfigPath != "" {
		args = append(args, "--config", cfg.ConfigPath)
	} else {
		args = append(args, "--addr", cfg.Addr, "--data", cfg.DataDir, "--disks", cfg.DisksDir,
			"--labs", cfg.LabsDir, "--instances", cfg.InstancesDir, "--logs", cfg.LogsDir,
			"--icons", cfg.IconsDir, "--db", cfg.DBPath, "--qemu", cfg.QEMUPath,
			"--socket", cfg.SocketPath, "--log", cfg.LogFile, "--restart-sec", strconv.Itoa(cfg.RestartSec))
	}
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Println(cmd.Process.Pid)
	return nil
}

func peerAllowed(conn net.Conn) bool {
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := unix.SyscallConn()
	if err != nil {
		return false
	}
	var uid uint32 = 0xffffffff
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			credErr = err
			return
		}
		uid = cred.Uid
	}); err != nil || credErr != nil {
		return false
	}
	return uid == 0 || uid == uint32(os.Getuid())
}
