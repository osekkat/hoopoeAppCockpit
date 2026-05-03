package api

import (
	"encoding/json"
	"net/http"
	"strings"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		http.Error(w, "internal encoding error", http.StatusInternalServerError)
	}
}

func writeProblem(w http.ResponseWriter, status int, title string, detail string) {
	writeProblemCode(w, status, problemCode(title), title, detail)
}

func writeProblemCode(w http.ResponseWriter, status int, code string, title string, detail string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	var detailPtr *string
	if detail != "" {
		detailPtr = &detail
	}
	_ = json.NewEncoder(w).Encode(schemas.Problem{
		Type:   "urn:hoopoe:problem:" + strings.ReplaceAll(code, ".", "-"),
		Title:  title,
		Status: status,
		Code:   code,
		Detail: detailPtr,
	})
}

func problemCode(title string) string {
	code := strings.ToLower(strings.TrimSpace(title))
	if code == "" {
		return "daemon.error"
	}
	var b strings.Builder
	lastDot := false
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDot = false
		case r >= '0' && r <= '9':
			if b.Len() == 0 {
				b.WriteString("daemon")
				b.WriteByte('.')
			}
			b.WriteRune(r)
			lastDot = false
		default:
			if b.Len() > 0 && !lastDot {
				b.WriteByte('.')
				lastDot = true
			}
		}
	}
	return strings.Trim(b.String(), ".")
}
