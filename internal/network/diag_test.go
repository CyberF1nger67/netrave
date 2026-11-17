// SPDX-License-Identifier: GPL-3.0-only

package network

import (
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/qemu"
)

// Diagnostics must not require root and must not create rows in the database: it only
// maps machine interfaces to the expected bridges and actual TAPs.
func TestDiagnosticsMapsInterfacesToBridges(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "diag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES(1,'admin','x','admin')`); err != nil {
		t.Fatal(err)
	}
	lab := labtest.Lab(t, db, 1, "Lab", "none")
	dev := labtest.Device(t, db, lab, labfile.Device{Name: "R1", NICs: 2, NICModel: "vmxnet3"})
	net := labtest.Network(t, db, lab, labfile.Network{Name: "SW", Type: "isolated"})
	labtest.Connection(t, db, lab, labfile.Connection{
		A: labfile.End{Type: "device", ID: dev, If: 0},
		B: labfile.End{Type: "network", ID: net, If: 0},
	})
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO lab_instances(id,lab_id,user_id) VALUES(20,?,1)`, []any{lab}},
		{`INSERT INTO instance_devices(id,instance_id,device_id,nics,nic_model,iface_type,iface_start)
			VALUES(500,20,?,2,'vmxnet3','te',1)`, []any{dev}},
		{`INSERT INTO instance_networks(id,instance_id,network_id,subnet,gateway,dhcp_start,dhcp_end)
			VALUES(7,20,?,'','','','')`, []any{net}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%v: %s", err, q.sql)
		}
	}
	nm := New(db, &config.Config{})
	qm := qemu.New(db, &config.Config{}, nil)
	qm.SetRuntime(500, qemu.Runtime{State: "running"})
	nm.SetQEMU(qm)
	d := nm.Diagnostics(lab)
	if len(d.Interfaces) != 2 {
		t.Fatalf("интерфейсов: %d", len(d.Interfaces))
	}
	first := d.Interfaces[0]
	if first.Expected != "nrn7" || first.Label != "te1" || first.NIC != 0 {
		t.Fatalf("интерфейс 0: %+v", first)
	}
	if first.MAC != qemu.MAC(500, 0) || !first.CableUp {
		t.Fatalf("интерфейс 0 (MAC/провод): %+v", first)
	}
	second := d.Interfaces[1]
	if second.Label != "te2" || !strings.HasPrefix(second.Expected, "(") {
		t.Fatalf("интерфейс 1 без провода: %+v", second)
	}
	if len(d.Bridges) != 1 || d.Bridges[0].Name != "nrn7" {
		t.Fatalf("мосты: %+v", d.Bridges)
	}
	if !strings.Contains(strings.Join(d.Warnings, "|"), "TAP отсутствует") {
		t.Fatalf("нет предупреждения об отсутствующем TAP: %v", d.Warnings)
	}
}
