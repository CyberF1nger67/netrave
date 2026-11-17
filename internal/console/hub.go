// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"bytes"
	"net"
	"strconv"
	"sync"
	"time"
)

// QEMU accepts only one connection on the serial port (tcp:...,server,nowait). To let the owner,
// administrators and several windows connect to one terminal at the same time, the server keeps ONE connection
// to QEMU per machine (serialHub) and shares its output with all clients. Input from any client is written to the same connection,
// so nobody blocks or disconnects anybody. A new client first receives recent output (scrollback).

const (
	scrollMax   = 64 << 10        // how much recent output to keep for new clients
	scrollKeep  = 48 << 10        // down to what size to trim on overflow
	clientQueue = 1024            // frame queue per client; a slow client is disconnected and does not slow down the others
	idleGrace   = 2 * time.Minute // how long to keep the QEMU serial port after the last client leaves
)

type outMsg struct {
	op   byte
	data []byte
}

type hubClient struct {
	hub    *serialHub
	out    chan outMsg
	done   chan struct{}
	mu     sync.Mutex
	ws     *wsConn
	killed bool
}

type serialHub struct {
	key     string
	h       *Handler
	back    net.Conn
	wmu     sync.Mutex // writes to QEMU are not interleaved between clients
	mu      sync.Mutex // clients, scroll, total, idle
	clients map[*hubClient]struct{}
	scroll  []byte
	total   int64 // absolute output counter since connecting to QEMU
	idle    *time.Timer
}

// attach connects a new client to the machine's hub (creates the hub and the QEMU connection on the first client).
// resume=true means the browser has already shown output and remembers the absolute offset after: it is sent
// only the missed tail. If the gap is too large, the client gets a new offset and continues without clearing
// the screen - the terminal must not visually "flicker" when the connection is restored.
func (h *Handler) attach(key string, port int, after int64, resume bool) (*hubClient, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hub := h.hubs[key]
	if hub == nil {
		back, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 3*time.Second)
		if err != nil {
			return nil, err
		}
		hub = &serialHub{key: key, h: h, back: back, clients: map[*hubClient]struct{}{}}
		h.hubs[key] = hub
		go hub.pump()
	}
	c := &hubClient{hub: hub, out: make(chan outMsg, clientQueue), done: make(chan struct{})}
	hub.mu.Lock()
	if hub.idle != nil {
		hub.idle.Stop()
		hub.idle = nil
	}
	total := hub.total
	if resume {
		start := total - int64(len(hub.scroll))
		switch {
		case after >= start && after <= total:
			skip := after - start
			if skip < int64(len(hub.scroll)) {
				c.out <- outMsg{opBinary, append([]byte(nil), hub.scroll[skip:]...)}
			}
		default:
			c.out <- outMsg{opText, []byte("netrave:sync:" + strconv.FormatInt(total, 10))}
		}
	} else {
		// New session: show recent history, then report the absolute end of history.
		c.out <- outMsg{opText, []byte("netrave:replay")}
		if len(hub.scroll) > 0 {
			c.out <- outMsg{opBinary, append([]byte(nil), hub.scroll...)}
		}
		c.out <- outMsg{opText, []byte("netrave:offset:" + strconv.FormatInt(total, 10))}
	}
	hub.clients[c] = struct{}{}
	hub.mu.Unlock()
	return c, nil
}

// detach disconnects the client. The QEMU connection lives for some time after the last client leaves:
// this way a short network drop does not lose the machine output and does not recreate the serial session.
func (h *Handler) detach(c *hubClient) {
	c.kill()
	hub := c.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	hub.mu.Lock()
	delete(hub.clients, c)
	if len(hub.clients) == 0 && h.hubs[hub.key] == hub {
		if hub.idle != nil {
			hub.idle.Stop()
		}
		hub.idle = time.AfterFunc(idleGrace, func() { h.expire(hub) })
	}
	hub.mu.Unlock()
}

// expire closes an idle hub. The double check under both locks prevents
// closing the port if a client came back between the timer and its firing.
func (h *Handler) expire(hub *serialHub) {
	h.mu.Lock()
	if h.hubs[hub.key] != hub {
		h.mu.Unlock()
		return
	}
	hub.mu.Lock()
	if len(hub.clients) > 0 {
		hub.idle = nil
		hub.mu.Unlock()
		h.mu.Unlock()
		return
	}
	delete(h.hubs, hub.key)
	hub.idle = nil
	hub.mu.Unlock()
	h.mu.Unlock()
	hub.back.Close()
}

// pump reads QEMU output and broadcasts it to all clients.
func (hub *serialHub) pump() {
	buf := make([]byte, 16*1024)
	for {
		n, err := hub.back.Read(buf)
		if n > 0 {
			hub.broadcast(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	hub.shutdown()
}

func (hub *serialHub) broadcast(data []byte) {
	var slow []*hubClient
	hub.mu.Lock()
	hub.total += int64(len(data))
	hub.scroll = append(hub.scroll, data...)
	if len(hub.scroll) > scrollMax {
		s := hub.scroll[len(hub.scroll)-scrollKeep:]
		// Start at a line / UTF-8 character boundary so a truncated control sequence is not shown.
		if i := bytes.IndexByte(s[:min(len(s), 1024)], '\n'); i >= 0 {
			s = s[i+1:]
		}
		for len(s) > 0 && s[0]&0xC0 == 0x80 {
			s = s[1:]
		}
		hub.scroll = append([]byte(nil), s...)
	}
	for c := range hub.clients {
		select {
		case c.out <- outMsg{opBinary, data}:
		default:
			slow = append(slow, c)
		}
	}
	hub.mu.Unlock()
	for _, c := range slow {
		c.kill()
	}
}

// shutdown: QEMU closed the port (the machine stopped) - disconnect all clients.
func (hub *serialHub) shutdown() {
	h := hub.h
	h.mu.Lock()
	if h.hubs[hub.key] == hub {
		delete(h.hubs, hub.key)
	}
	h.mu.Unlock()
	hub.mu.Lock()
	if hub.idle != nil {
		hub.idle.Stop()
		hub.idle = nil
	}
	list := make([]*hubClient, 0, len(hub.clients))
	for c := range hub.clients {
		list = append(list, c)
	}
	hub.mu.Unlock()
	hub.back.Close()
	for _, c := range list {
		c.kill()
	}
}

// write sends client input to QEMU entirely, without interleaving with other clients' input.
func (hub *serialHub) write(data []byte) error {
	hub.wmu.Lock()
	defer hub.wmu.Unlock()
	_ = hub.back.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := hub.back.Write(data)
	return err
}

func (c *hubClient) setWS(ws *wsConn) {
	c.mu.Lock()
	c.ws = ws
	killed := c.killed
	c.mu.Unlock()
	if killed {
		ws.closeConn()
	}
}

// kill closes only this client connection; other clients and QEMU are not affected.
func (c *hubClient) kill() {
	c.mu.Lock()
	if c.killed {
		c.mu.Unlock()
		return
	}
	c.killed = true
	ws := c.ws
	close(c.done)
	c.mu.Unlock()
	if ws != nil {
		ws.closeConn()
	}
}

// run: while the client is connected - passes output to the browser (a separate goroutine) and input from the browser to QEMU.
func (h *Handler) runSerial(c *hubClient) {
	go func() {
		for {
			select {
			case <-c.done:
				return
			case m := <-c.out:
				if err := c.ws.writeFrame(m.op, m.data); err != nil {
					c.kill()
					return
				}
			}
		}
	}()
	for {
		op, data, err := c.ws.readMessage()
		if err != nil {
			break
		}
		if handleControl(c.ws, op, data) {
			continue
		}
		if (op == opText || op == opBinary) && len(data) > 0 {
			if werr := c.hub.write(data); werr != nil {
				break
			}
		}
	}
	h.detach(c)
}
