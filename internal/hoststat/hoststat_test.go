// SPDX-License-Identifier: GPL-3.0-only

package hoststat

import (
	"strings"
	"testing"
)

// Aggregate cpu line: user=100 nice=0 system=50 idle=800 iowait=50 irq=0 softirq=0 steal=0.
// 1000 ticks in total, 850 idle, 150 busy.
func TestParseStat(t *testing.T) {
	in := "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 50 0 25 400 25 0 0 0 0 0\nintr 1\n"
	busy, total, err := ParseStat(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if total != 1000 || busy != 150 {
		t.Fatalf("expected busy=150 total=1000, got busy=%d total=%d", busy, total)
	}
}

func TestParseStatWithoutCPULine(t *testing.T) {
	if _, _, err := ParseStat(strings.NewReader("intr 1\n")); err == nil {
		t.Fatal("missing cpu line should return error")
	}
}

// The first sample is only the baseline, the second gives the share of busy time over the interval.
func TestCPUPercentFromSamples(t *testing.T) {
	c := New()
	if _, ok := c.Percent(); ok {
		t.Fatal("no value expected before two samples")
	}
	c.apply(150, 1000)
	if _, ok := c.Percent(); ok {
		t.Fatal("no value expected after one sample")
	}
	// Over the interval 200 ticks were added, 100 of them busy: 50%.
	c.apply(250, 1200)
	pct, ok := c.Percent()
	if !ok || pct != 50 {
		t.Fatalf("expected 50%%, got %v (ok=%v)", pct, ok)
	}
}
