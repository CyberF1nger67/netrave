// SPDX-License-Identifier: GPL-3.0-only

// Package hoststat measures the whole server's CPU load from /proc/stat and checks
// whether the QEMU process is alive. No mock values: only Linux kernel metrics.
package hoststat

import (
	"bufio"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPU keeps the last measured CPU load of the whole server.
type CPU struct {
	mu        sync.Mutex
	pct       float64
	ok        bool
	havePrev  bool
	prevBusy  uint64
	prevTotal uint64
}

// New creates a meter. Until the second sample, Percent returns ok=false.
func New() *CPU { return &CPU{} }

// Run polls /proc/stat once a second until the context is cancelled.
func (c *CPU) Run(ctx context.Context) {
	c.sample()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.sample()
		}
	}
}

func (c *CPU) sample() {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	busy, total, err := ParseStat(f)
	if err != nil {
		return
	}
	c.apply(busy, total)
}

// apply accounts for the next sample: the load is the share of busy time over the interval between samples.
func (c *CPU) apply(busy, total uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.havePrev && total > c.prevTotal && busy >= c.prevBusy {
		pct := float64(busy-c.prevBusy) / float64(total-c.prevTotal) * 100
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		c.pct, c.ok = pct, true
	}
	c.prevBusy, c.prevTotal, c.havePrev = busy, total, true
}

// Percent returns the CPU load in percent over the last interval.
func (c *CPU) Percent() (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pct, c.ok
}

// ParseStat reads the aggregate "cpu" line from /proc/stat and returns busy and total time in ticks.
// Idle and disk wait (iowait) count as non-busy time.
// The guest column is already included in user, so it is not added to the sum.
func ParseStat(r io.Reader) (busy, total uint64, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var v [8]uint64
		for i := 0; i < 8 && i+1 < len(f); i++ {
			n, perr := strconv.ParseUint(f[i+1], 10, 64)
			if perr != nil {
				return 0, 0, perr
			}
			v[i] = n // user nice system idle iowait irq softirq steal
		}
		for _, x := range v {
			total += x
		}
		idle := v[3] + v[4]
		return total - idle, total, nil
	}
	return 0, 0, io.ErrUnexpectedEOF
}
