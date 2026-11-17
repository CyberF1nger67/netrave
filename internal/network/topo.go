// SPDX-License-Identifier: GPL-3.0-only

package network

import "netrave/internal/labfile"

// Lab topology (devices, networks, cables) is read from the lab file through the labfile store.
// Here are small helpers for the queries that used to be done with SQL.

// connsOf returns the lab's cables in id order.
func (m *Manager) connsOf(labID int64) []labfile.Connection {
	l, ok := m.db.Labs.Lab(labID)
	if !ok {
		return nil
	}
	return l.Connections
}

// connRow returns a cable by id.
func (m *Manager) connRow(id int64) (labfile.Connection, bool) {
	c, _, ok := m.db.Labs.Connection(id)
	return c, ok
}

// connAtPort returns the first cable connected to port nic of device devID.
func connAtPort(conns []labfile.Connection, devID int64, nic int) (labfile.Connection, bool) {
	for _, c := range conns {
		if (c.A.Type == "device" && c.A.ID == devID && c.A.If == nic) || (c.B.Type == "device" && c.B.ID == devID && c.B.If == nic) {
			return c, true
		}
	}
	return labfile.Connection{}, false
}
