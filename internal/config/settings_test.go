// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUpdateSettingsPersistsAndKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("addr: \":9090\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionTTL().Hours() != 24 || c.MaxUploadBytes() != 20480<<20 {
		t.Fatalf("default values: %v %d", c.SessionTTL(), c.MaxUploadBytes())
	}
	if err := c.UpdateSettings(map[string]string{"session_ttl_hours": "2", "max_upload_mb": "100"}); err != nil {
		t.Fatal(err)
	}
	if c.SessionTTL().Hours() != 2 || c.MaxUploadBytes() != 100<<20 {
		t.Fatalf("not updated in memory: %v %d", c.SessionTTL(), c.MaxUploadBytes())
	}
	if err := c.SetRoleAdminDescription("description"); err != nil {
		t.Fatal(err)
	}
	// The file is re-read: settings are saved, other keys are not lost.
	c2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Addr != ":9090" || c2.Settings.SessionTTLHours != 2 || c2.Settings.MaxUploadMB != 100 || c2.Settings.RoleAdminDescription != "description" {
		t.Fatalf("after reload: %+v", c2)
	}
	var doc map[string]any
	data, _ := os.ReadFile(path)
	if err := yaml.Unmarshal(data, &doc); err != nil || doc["settings"] == nil {
		t.Fatalf("settings section not written: %s", data)
	}
}

func TestUpdateSettingsWithoutFile(t *testing.T) {
	c := &Config{}
	if err := c.UpdateSettings(map[string]string{"max_upload_mb": "10"}); !errors.Is(err, ErrNoConfigFile) {
		t.Fatalf("expected an error without config file: %v", err)
	}
	if c.MaxUploadBytes() != 20480<<20 {
		t.Fatal("memory must not change on save error")
	}
}

func TestSettingValid(t *testing.T) {
	if known, ok := SettingValid("session_ttl_hours", "721"); !known || ok {
		t.Fatal("hours above 720 must be rejected")
	}
	if known, _ := SettingValid("unknown", "1"); known {
		t.Fatal("unknown setting must not be treated as known")
	}
}
