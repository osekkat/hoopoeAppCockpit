package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := encodeJSON(payload)
	if err != nil {
		writeProblemCode(w, http.StatusInternalServerError, "daemon.encoding_failed", "internal encoding error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeProblem(w http.ResponseWriter, status int, title string, detail string) {
	writeProblemCode(w, status, problemCode(title), title, detail)
}

func writeProblemCode(w http.ResponseWriter, status int, code string, title string, detail string) {
	var detailPtr *string
	if detail != "" {
		detailPtr = &detail
	}
	body, err := encodeJSON(schemas.Problem{
		Type:   "urn:hoopoe:problem:" + strings.ReplaceAll(code, ".", "-"),
		Title:  title,
		Status: status,
		Code:   code,
		Detail: detailPtr,
	})
	if err != nil {
		body = []byte(`{"type":"urn:hoopoe:problem:daemon-encoding-failed","title":"internal encoding error","status":500,"code":"daemon.encoding_failed"}` + "\n")
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func encodeJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
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
