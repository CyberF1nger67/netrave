// SPDX-License-Identifier: GPL-3.0-only

package database

import "strings"

// TemplateCategory - a template group in the catalog (machine selection menu and the "Templates" page).
type TemplateCategory struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// TemplateCategories - valid template categories in display order.
var TemplateCategories = []TemplateCategory{
	{"cisco", "Cisco"},
	{"mikrotik", "MikroTik"},
	{"linux", "Linux"},
	{"windows", "Windows"},
	{"firewall", "Firewall"},
	{"network", "Сеть и коммутация"},
	{"server", "Серверы"},
	{"other", "Другие"},
}

// ValidCategory reports whether such a category is in the list.
func ValidCategory(key string) bool {
	for _, c := range TemplateCategories {
		if c.Key == key {
			return true
		}
	}
	return false
}

// CategoryFor chooses a category by template vendor and type. Used when the category is not set explicitly.
func CategoryFor(vendor, typ string) string {
	v := strings.ToLower(vendor)
	switch {
	case strings.Contains(v, "cisco"):
		return "cisco"
	case strings.Contains(v, "mikrotik"):
		return "mikrotik"
	case strings.Contains(v, "microsoft") || typ == "windows":
		return "windows"
	case typ == "firewall":
		return "firewall"
	case typ == "linux" || v == "alpine" || v == "ubuntu" || v == "debian":
		return "linux"
	case typ == "server":
		return "server"
	case typ == "router" || typ == "switch":
		return "network"
	default:
		return "other"
	}
}
