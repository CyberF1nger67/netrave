// SPDX-License-Identifier: GPL-3.0-only

package yamlfmt

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type point struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
	N string  `yaml:"n,omitempty"`
	V string  `yaml:"value"`
}

func TestMarshalKeepsPlainKeys(t *testing.T) {
	data, err := Marshal(point{X: 16, Y: 10, N: "no", V: "y"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"x: 16\n", "y: 10\n", "n: \"no\"\n", "value: \"y\"\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"y":`) || strings.Contains(got, `"n":`) {
		t.Errorf("keys remain quoted:\n%s", got)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := point{X: 1.5, Y: -2, V: "a"}
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out point
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round trip: %+v != %+v", out, in)
	}
}
