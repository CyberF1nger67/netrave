// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// qmpDo opens a short connection to the QMP socket, executes one command and closes it.
func qmpDo(path, cmd string, args any, timeout time.Duration) (json.RawMessage, error) {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	r := bufio.NewReader(conn)
	if _, err := r.ReadBytes('\n'); err != nil { // greeting
		return nil, err
	}
	send := func(c string, a any) (json.RawMessage, error) {
		req := map[string]any{"execute": c}
		if a != nil {
			req["arguments"] = a
		}
		b, _ := json.Marshal(req)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return nil, err
		}
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return nil, err
			}
			var resp struct {
				Return json.RawMessage `json:"return"`
				Event  string          `json:"event"`
				Error  *struct {
					Class string `json:"class"`
					Desc  string `json:"desc"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &resp); err != nil {
				return nil, err
			}
			if resp.Event != "" {
				continue
			}
			if resp.Error != nil {
				return nil, fmt.Errorf("QMP %s: %s", c, resp.Error.Desc)
			}
			return resp.Return, nil
		}
	}
	if _, err := send("qmp_capabilities", nil); err != nil {
		return nil, err
	}
	return send(cmd, args)
}

// qmpDeviceExists checks whether a device with the given qdev id exists among peripherals.
func qmpDeviceExists(path, id string) bool {
	out, err := qmpDo(path, "qom-list", map[string]string{"path": "/machine/peripheral"}, 2*time.Second)
	if err != nil {
		return false
	}
	var items []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(out, &items) != nil {
		return false
	}
	for _, it := range items {
		if it.Name == id {
			return true
		}
	}
	return false
}

func qmpStatus(path string) (string, error) {
	out, err := qmpDo(path, "query-status", nil, 2*time.Second)
	if err != nil {
		return "", err
	}
	var st struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return "", err
	}
	return st.Status, nil
}

func stateFromQMP(st string) string {
	switch st {
	case "running":
		return "running"
	case "paused", "prelaunch", "suspended", "inmigrate", "postmigrate", "save-vm", "restore-vm", "guest-panicked", "io-error", "watchdog":
		return "paused"
	default:
		return "running"
	}
}
