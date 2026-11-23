// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"os/exec"
	"testing"
	"time"

	"netrave/internal/config"
)

// CPU load is calculated from the process's actual CPU time (/proc/<pid>/stat).
// A process burning one core should show noticeable load; after exit - 0 and "not measured".
func TestStatsMeasuresRealCPUAndClearsAfterExit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Skipf("no shell for test: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	m := New(nil, &config.Config{}, nil)
	m.vms[7] = &vm{pid: cmd.Process.Pid, done: make(chan struct{})}

	m.Stats([]int64{7}) // first sample: baseline
	time.Sleep(1200 * time.Millisecond)
	st := m.Stats([]int64{7})[7]
	if !st.Ready {
		t.Fatalf("after the second sample, load must be measured")
	}
	if st.CPU < 50 {
		t.Fatalf("process burns the core, expected >= 50%%, got %.1f%%", st.CPU)
	}
	if st.RSS == 0 {
		t.Fatalf("process memory must be read from /proc, got 0")
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	time.Sleep(50 * time.Millisecond)
	st = m.Stats([]int64{7})[7]
	if st.CPU != 0 || st.Ready {
		t.Fatalf("after the process exits, old load must not remain: %+v", st)
	}
}
