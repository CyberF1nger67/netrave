// SPDX-License-Identifier: GPL-3.0-only

// Package yamlfmt writes YAML files of the application. It is yaml.v3 plus one fix: yaml.v3
// quotes keys such as "y" or "no" because YAML 1.1 reads them as booleans. In netrave these
// words are plain strings (a machine coordinate is "y"), so the quotes only make the files
// look inconsistent: `x: 16` next to `"y": 10`. The quotes are removed for keys only.
package yamlfmt

import (
	"regexp"

	"gopkg.in/yaml.v3"
)

// reservedKey matches a double-quoted mapping key that yaml.v3 quoted for YAML 1.1 compatibility.
var reservedKey = regexp.MustCompile(`(?m)^(\s*(?:- )?)"(y|Y|n|N|yes|Yes|YES|no|No|NO|on|On|ON|off|Off|OFF)":`)

// Marshal encodes v like yaml.Marshal, without quotes around plain keys such as y and n.
func Marshal(v any) ([]byte, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return reservedKey.ReplaceAll(data, []byte("${1}${2}:")), nil
}
