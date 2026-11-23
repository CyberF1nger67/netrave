// SPDX-License-Identifier: GPL-3.0-only

// Package msgs carries user-facing errors as an English key with {name} parameters.
// The interface translates the key; Error() renders the English text for logs and tests.
package msgs

import (
	"errors"
	"strings"
)

// Error is an error whose message is a key and its parameters.
type Error struct {
	Key    string
	Params map[string]string
	Cause  error
}

// Error renders the key with its parameters in English.
func (e *Error) Error() string {
	return Render(e.Key, e.Params)
}

// Render substitutes the parameters into the key. A parameter whose name ends in _key is itself a key,
// rendered with the other parameters (the same rule as the interface uses).
func Render(key string, params map[string]string) string {
	plain := map[string]string{}
	for k, v := range params {
		if !strings.HasSuffix(k, "_key") {
			plain[k] = v
		}
	}
	s := key
	for k, v := range params {
		if strings.HasSuffix(k, "_key") {
			v = Render(v, plain)
		}
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// Unwrap returns the underlying cause, so errors.Is and errors.As still work.
func (e *Error) Unwrap() error { return e.Cause }

// New makes a keyed error without a cause.
func New(key string, params map[string]string) error {
	return &Error{Key: key, Params: params}
}

// Wrap makes a keyed error that keeps cause.
func Wrap(cause error, key string, params map[string]string) error {
	return &Error{Key: key, Params: params, Cause: cause}
}

// Lookup returns the key and parameters of err when it carries them.
func Lookup(err error) (string, map[string]string, bool) {
	var m *Error
	if errors.As(err, &m) {
		return m.Key, m.Params, true
	}
	return "", nil, false
}

// Message is a text the interface translates: an English key with {name} parameters.
// It goes out in JSON as {"key": ..., "params": {...}}. Used for warnings and lists of problems.
type Message struct {
	Key    string            `json:"key"`
	Params map[string]string `json:"params,omitempty"`
}

// M makes a Message from a key and its parameters.
func M(key string, params map[string]string) Message {
	return Message{Key: key, Params: params}
}

// Of returns err as a Message: a keyed error keeps its key and parameters, any other goes out as its text.
func Of(err error) Message {
	if key, params, ok := Lookup(err); ok {
		return Message{Key: key, Params: params}
	}
	return Message{Key: err.Error()}
}

// Join renders the messages in English and joins them with sep, for a text that has no key of its own.
func Join(list []Message, sep string) string {
	parts := make([]string, 0, len(list))
	for _, m := range list {
		parts = append(parts, Render(m.Key, m.Params))
	}
	return strings.Join(parts, sep)
}

// Merge returns one map with the parameters of all the arguments; later maps win.
func Merge(parts ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}
