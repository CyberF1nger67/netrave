// SPDX-License-Identifier: GPL-3.0-only

package images

import (
	"encoding/json"
	"testing"
)

// An image without QEMU parameters must not return qemu as null: the UI reads the fields of this object.
func TestParseQemuNeverNil(t *testing.T) {
	for _, raw := range []string{"", "null", "битый json", "[]"} {
		got := parseQemu(raw)
		if got == nil {
			t.Fatalf("parseQemu(%q) = nil, want empty map", raw)
		}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "{}" {
			t.Fatalf("parseQemu(%q) marshals to %s, want {}", raw, data)
		}
	}
}

func TestParseQemuKeepsValues(t *testing.T) {
	got := parseQemu(`{"machine":"q35","bios":"ovmf"}`)
	if got["machine"] != "q35" || got["bios"] != "ovmf" {
		t.Fatalf("unexpected parse result: %v", got)
	}
}
