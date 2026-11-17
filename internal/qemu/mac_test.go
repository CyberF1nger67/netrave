// SPDX-License-Identifier: GPL-3.0-only

package qemu

import "testing"

// MAC is deterministic: the same machine interface always gets one address, different ones
// differ. The address is local (bit 0x02) and not group (bit 0x01 of the first byte = 0).
func TestMACIsDeterministicAndUnique(t *testing.T) {
	if got := MAC(7, 1); got != "52:54:00:00:07:01" {
		t.Fatalf("MAC(7,1) = %s", got)
	}
	if MAC(7, 1) != MAC(7, 1) {
		t.Fatal("адрес должен быть стабильным")
	}
	seen := map[string]bool{}
	for dev := int64(1); dev <= 50; dev++ {
		for nic := 0; nic < 32; nic++ {
			m := MAC(dev, nic)
			if seen[m] {
				t.Fatalf("повтор адреса %s", m)
			}
			seen[m] = true
		}
	}
}
