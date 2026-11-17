// SPDX-License-Identifier: GPL-3.0-only

package catalog

import "errors"

// VNCQuality - VNC console image settings set by a machine template.
// nil in numeric fields and "auto" in string fields mean "Auto".
type VNCQuality struct {
	Resolution  string `yaml:"resolution" json:"resolution"`
	Bitrate     *int   `yaml:"bitrate" json:"bitrate"`
	Quality     *int   `yaml:"quality" json:"quality"`
	Compression *int   `yaml:"compression" json:"compression"`
	FPS         *int   `yaml:"fps" json:"fps"`
	Encoding    string `yaml:"encoding" json:"encoding"`
}

// vncResolutions are the resolutions offered by the UI. Width 1366 is not a multiple of 8:
// QEMU may round it, and the client will show the actual resolution.
var vncResolutions = map[string]bool{
	"auto": true, "640x480": true, "800x600": true, "1024x768": true,
	"1280x720": true, "1366x768": true, "1600x900": true, "1920x1080": true,
}

var vncEncodings = map[string]bool{"auto": true, "tight": true, "hextile": true, "raw": true}

// DefaultVNCQuality - all parameters set to "Auto".
func DefaultVNCQuality() VNCQuality {
	return VNCQuality{Resolution: "auto", Encoding: "auto"}
}

// Normalized fills empty string fields with "auto". Old records without a vnc field
// get fully automatic settings.
func (q VNCQuality) Normalized() VNCQuality {
	if q.Resolution == "" {
		q.Resolution = "auto"
	}
	if q.Encoding == "" {
		q.Encoding = "auto"
	}
	return q
}

func inRange(value *int, min, max int) bool {
	return value == nil || (*value >= min && *value <= max)
}

// Validate checks the normalized settings. Arbitrary strings and numbers do not reach
// either the template or the VNC protocol.
func (q VNCQuality) Validate() error {
	q = q.Normalized()
	switch {
	case !vncResolutions[q.Resolution]:
		return errors.New("недопустимое разрешение VNC")
	case !inRange(q.Bitrate, 1, 100):
		return errors.New("битрейт VNC должен быть от 1 до 100 Мбит/с")
	case !inRange(q.Quality, 1, 9):
		return errors.New("качество JPEG VNC должно быть от 1 до 9")
	case !inRange(q.Compression, 1, 9):
		return errors.New("сжатие VNC должно быть от 1 до 9")
	case !inRange(q.FPS, 1, 60):
		return errors.New("частота кадров VNC должна быть от 1 до 60")
	case !vncEncodings[q.Encoding]:
		return errors.New("неизвестный режим кодирования VNC")
	}
	return nil
}
