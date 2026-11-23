// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"netrave/internal/auth"
	"netrave/internal/notify"
)

// Messages from administrators. An administrator writes a short text to all active users or to selected
// ones. Each message is delivered as a notification in the notification centre, so the recipient sees it
// in real time when connected. The text is passed as a parameter, not as the notification body template:
// it is shown as written and is never treated as a translation key.

const (
	maxMessageRunes      = 2000
	maxMessageRecipients = 500
	messageTitle         = "Message from {sender}"
	messageBody          = "{text}"
)

// sendMessage handles POST /api/messages. Body: {"text", "level" (info or warning), "all", "user_ids"}.
// Either all=true (every active user) or a non-empty list of user identifiers is required.
func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    string  `json:"text"`
		Level   string  `json:"level"`
		All     bool    `json:"all"`
		UserIDs []int64 `json:"user_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		writeErr(w, http.StatusBadRequest, "Enter the message text")
		return
	}
	if utf8.RuneCountInString(text) > maxMessageRunes {
		writeErr(w, http.StatusBadRequest, "Message text: up to 2000 characters")
		return
	}
	level := in.Level
	if level == "" {
		level = notify.Info
	}
	if level != notify.Info && level != notify.Warning {
		writeErr(w, http.StatusBadRequest, "Message level: info or warning")
		return
	}
	if !in.All && len(in.UserIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "Choose the recipients")
		return
	}
	if len(in.UserIDs) > maxMessageRecipients {
		writeErr(w, http.StatusBadRequest, "Too many recipients")
		return
	}

	sender := auth.FromContext(r.Context()).User.Username
	params := notify.P("sender", sender, "text", text)

	sent := 0
	if in.All {
		// SendActive skips disabled accounts; the count is taken with the same rule.
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE disabled=0`).Scan(&sent); err != nil {
			writeErr(w, http.StatusInternalServerError, "Could not read the recipients")
			return
		}
		h.Notify.SendActive(0, level, notify.CatMessage, messageTitle, messageBody, "", params)
	} else {
		seen := map[int64]bool{}
		for _, id := range in.UserIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			var uid int64
			if err := h.DB.QueryRow(`SELECT id FROM users WHERE id=? AND disabled=0`, id).Scan(&uid); err != nil {
				continue
			}
			h.Notify.Send(uid, level, notify.CatMessage, messageTitle, messageBody, "", params)
			sent++
		}
		if sent == 0 {
			writeErr(w, http.StatusBadRequest, "No active recipients")
			return
		}
	}

	h.auditKey(r, "message_send", "Message sent to {count} users", map[string]string{"count": strconv.Itoa(sent)})
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent})
}
