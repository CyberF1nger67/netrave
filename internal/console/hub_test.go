// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"testing"
	"time"
)

func testHub(t *testing.T, output string) (*Handler, *serialHub) {
	t.Helper()
	h := &Handler{hubs: map[string]*serialHub{}}
	hub := &serialHub{key: "serial:1", h: h, clients: map[*hubClient]struct{}{}}
	h.hubs[hub.key] = hub
	if output != "" {
		hub.broadcast([]byte(output))
	}
	return h, hub
}

func nextOut(t *testing.T, c *hubClient) outMsg {
	t.Helper()
	select {
	case message := <-c.out:
		return message
	case <-time.After(time.Second):
		t.Fatal("no queued console frame")
		return outMsg{}
	}
}

func TestSerialInitialReplayCarriesAbsoluteOffset(t *testing.T) {
	h, _ := testHub(t, "0123456789")
	c, err := h.attach("serial:1", 1, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if message := nextOut(t, c); message.op != opText || string(message.data) != "netrave:replay" {
		t.Fatalf("replay marker: op=%#x data=%q", message.op, message.data)
	}
	if message := nextOut(t, c); message.op != opBinary || string(message.data) != "0123456789" {
		t.Fatalf("initial scrollback: op=%#x data=%q", message.op, message.data)
	}
	if message := nextOut(t, c); message.op != opText || string(message.data) != "netrave:offset:10" {
		t.Fatalf("initial offset: op=%#x data=%q", message.op, message.data)
	}
}

func TestSerialResumeSendsOnlyMissingSuffix(t *testing.T) {
	h, hub := testHub(t, "0123456789")
	c, err := h.attach("serial:1", 1, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if message := nextOut(t, c); message.op != opBinary || string(message.data) != "456789" {
		t.Fatalf("resume suffix: op=%#x data=%q", message.op, message.data)
	}
	select {
	case message := <-c.out:
		t.Fatalf("unexpected extra frame: op=%#x data=%q", message.op, message.data)
	default:
	}

	// Already up to date: neither terminal history nor a visual reset is sent.
	current, err := h.attach("serial:1", 1, hub.total, true)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-current.out:
		t.Fatalf("unexpected frame for current offset: op=%#x data=%q", message.op, message.data)
	default:
	}
}

func TestSerialResumeWithUnknownOffsetKeepsScreen(t *testing.T) {
	h, _ := testHub(t, "0123456789")
	c, err := h.attach("serial:1", 1, 99, true)
	if err != nil {
		t.Fatal(err)
	}
	if message := nextOut(t, c); message.op != opText || string(message.data) != "netrave:sync:10" {
		t.Fatalf("offset reset: op=%#x data=%q", message.op, message.data)
	}
	select {
	case message := <-c.out:
		t.Fatalf("unexpected replay without reset marker: op=%#x data=%q", message.op, message.data)
	default:
	}
}

func TestSerialBroadcastAdvancesTotalAndTrimsScrollback(t *testing.T) {
	h, hub := testHub(t, "")
	chunk := make([]byte, scrollMax+1024)
	for i := range chunk {
		chunk[i] = byte('a' + i%26)
	}
	hub.broadcast(chunk)
	hub.mu.Lock()
	total, length := hub.total, len(hub.scroll)
	hub.mu.Unlock()
	if total != int64(len(chunk)) {
		t.Fatalf("total=%d, want %d", total, len(chunk))
	}
	if length > scrollMax || length < scrollKeep {
		t.Fatalf("scroll length=%d, want between %d and %d", length, scrollKeep, scrollMax)
	}
	_ = h
}
