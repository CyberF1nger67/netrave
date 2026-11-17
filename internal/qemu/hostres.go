// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// MemAvailable returns the host's available RAM in bytes (0 if unknown).
func MemAvailable() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) >= 2 && fl[0] == "MemAvailable:" {
			kb, _ := strconv.ParseUint(fl[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// MemTotal returns the host's total RAM in bytes (0 if unknown).
func MemTotal() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) >= 2 && fl[0] == "MemTotal:" {
			kb, _ := strconv.ParseUint(fl[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// DiskFree returns the free space on the partition holding path.
func DiskFree(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return uint64(st.Bavail) * uint64(st.Bsize)
}

// PortFree checks that the local TCP port is not in use.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// FindQemuImg looks for qemu-img in PATH or next to the QEMU binary.
func FindQemuImg(qemuPath string) (string, error) {
	if p, err := exec.LookPath("qemu-img"); err == nil {
		return p, nil
	}
	if strings.Contains(qemuPath, "/") {
		c := filepath.Join(filepath.Dir(qemuPath), "qemu-img")
		if fileExists(c) {
			return c, nil
		}
	}
	return "", errors.New("qemu-img не найден (установите qemu-utils)")
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func readCPUTicks(pid int) (uint64, bool) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 > len(s) {
		return 0, false
	}
	f := strings.Fields(s[i+2:])
	if len(f) < 13 {
		return 0, false
	}
	ut, e1 := strconv.ParseUint(f[11], 10, 64)
	st, e2 := strconv.ParseUint(f[12], 10, 64)
	if e1 != nil || e2 != nil {
		return 0, false
	}
	return ut + st, true
}

func readRSS(pid int) uint64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fl := strings.Fields(line)
			if len(fl) >= 2 {
				kb, _ := strconv.ParseUint(fl[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

// cmdlineArgs reads the process arguments.
func cmdlineArgs(pid int) []string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

// deviceIDFromArgs returns the device id if the process is our QEMU (-name netrave-d<ID>,...).
func deviceIDFromArgs(args []string) (int64, bool) {
	if len(args) == 0 || !strings.HasPrefix(filepath.Base(args[0]), "qemu-system-") {
		return 0, false
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-name" && strings.HasPrefix(args[i+1], "netrave-d") {
			rest := strings.TrimPrefix(args[i+1], "netrave-d")
			if j := strings.Index(rest, ","); j >= 0 {
				rest = rest[:j]
			}
			if id, err := strconv.ParseInt(rest, 10, 64); err == nil {
				return id, true
			}
		}
	}
	return 0, false
}
