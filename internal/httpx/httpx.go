// SPDX-License-Identifier: GPL-3.0-only

package httpx

import (
	"encoding/json"
	"net/http"
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

// Decode parses the request JSON body with a size limit.
func Decode(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		Err(w, http.StatusBadRequest, "некорректный JSON")
		return false
	}
	return true
}
