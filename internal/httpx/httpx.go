// SPDX-License-Identifier: GPL-3.0-only

package httpx

import (
	"encoding/json"
	"net/http"

	"netrave/internal/msgs"
)

// JSON writes the value as JSON.
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Err writes an error as {"error": "..."}.
func Err(w http.ResponseWriter, code int, msg string) {
	JSON(w, code, map[string]string{"error": msg})
}

// ErrFrom writes err: an error that carries a key goes out as the key and its parameters, any other as text.
func ErrFrom(w http.ResponseWriter, code int, err error) {
	if key, params, ok := msgs.Lookup(err); ok {
		ErrKey(w, code, key, params)
		return
	}
	Err(w, code, err.Error())
}

// ErrKey writes an error whose text is an English key with {name} parameters; the interface translates it.
func ErrKey(w http.ResponseWriter, code int, key string, params map[string]string) {
	JSON(w, code, map[string]any{"error": key, "params": params})
}

// Decode parses the request JSON body with a size limit.
func Decode(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		Err(w, http.StatusBadRequest, "Invalid JSON")
		return false
	}
	return true
}
