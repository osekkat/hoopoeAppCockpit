package redaction

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

func defaultPatterns() []pattern {
	return []pattern{
		{
			id:    "http-header-authorization",
			regex: regexp.MustCompile(`(?im)\bAuthorization\s*:\s*[^\r\n]+`),
			replace: func(string) string {
				return "Authorization: [redacted-header]"
			},
		},
		{
			id:    "http-header-cookie",
			regex: regexp.MustCompile(`(?im)\bCookie\s*:\s*[^\r\n]+`),
			replace: func(string) string {
				return "Cookie: [redacted-header]"
			},
		},
		{
			id:    "http-header-set-cookie",
			regex: regexp.MustCompile(`(?im)\bSet-Cookie\s*:\s*[^\r\n]+`),
			replace: func(string) string {
				return "Set-Cookie: [redacted-header]"
			},
		},
		{
			id:    "private-key-block",
			regex: regexp.MustCompile(`(?s)-----BEGIN (?:RSA |OPENSSH |EC |PGP )?PRIVATE KEY(?: BLOCK)?-----.*?-----END (?:RSA |OPENSSH |EC |PGP )?PRIVATE KEY(?: BLOCK)?-----`),
			replace: func(string) string {
				return "[private-key-redacted]"
			},
		},
		{
			id:      "bearer-hmac",
			regex:   regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`),
			replace: hashTag,
		},
		{
			id:      "bearer-token",
			regex:   regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/\-]{16,}={0,2}\b`),
			replace: bearerReplace,
		},
		{
			id:      "provider-key-anthropic",
			regex:   regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{16,}\b`),
			replace: providerKeyReplace("sk-ant-"),
		},
		{
			id:      "provider-key-sk",
			regex:   regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}\b`),
			replace: providerKeyReplace("sk-"),
		},
		{
			id:      "provider-key-google",
			regex:   regexp.MustCompile(`\bAIza[A-Za-z0-9_\-]{35}\b`),
			replace: providerKeyReplace("AIza-"),
		},
		{
			id:      "provider-key-aws",
			regex:   regexp.MustCompile(`\bAKIA[A-Z0-9]{16}\b`),
			replace: providerKeyReplace("AKIA-"),
		},
		{
			id:      "provider-key-github",
			regex:   regexp.MustCompile(`\bghp_[A-Za-z0-9]{36,}\b`),
			replace: providerKeyReplace("ghp-"),
		},
		{
			id:      "provider-key-slack",
			regex:   regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}\b`),
			replace: providerKeyReplace("xox-"),
		},
		{
			id:    "pairing-token",
			regex: regexp.MustCompile(`\bH-[ABCDEFGHJKMNPQRSTVWXYZ0-9]{11}\b`),
			replace: func(string) string {
				return "[pairing-token-redacted]"
			},
		},
		{
			id:    "ssh-passphrase",
			regex: regexp.MustCompile(`(?i)\bpassphrase\s*[:=]\s*\S+`),
			replace: func(string) string {
				return "passphrase=[redacted]"
			},
		},
		{
			id:    "browser-cookie-chatgpt",
			regex: regexp.MustCompile(`(?i)\b__Secure-next-auth\.session-token\s*=\s*[A-Za-z0-9._\-]+`),
			replace: func(string) string {
				return "__Secure-next-auth.session-token=[redacted]"
			},
		},
		{
			id:    "browser-cookie-claude",
			regex: regexp.MustCompile(`(?i)\b(?:claude|anthropic)[\w.\-]*session[\w.\-]*\s*=\s*[A-Za-z0-9._\-]+`),
			replace: func(string) string {
				return "claude-session=[redacted]"
			},
		},
		{
			id:    "browser-cookie-oai",
			regex: regexp.MustCompile(`(?i)\boai[\w.\-]*session[\w.\-]*\s*=\s*[A-Za-z0-9._\-]+`),
			replace: func(string) string {
				return "oai-session=[redacted]"
			},
		},
		{
			id:    "telegram-bot-token",
			regex: regexp.MustCompile(`\b\d{8,10}:[A-Za-z0-9_\-]{30,}\b`),
			replace: func(string) string {
				return "[telegram-bot-token-redacted]"
			},
		},
		{
			id:      "email-address",
			regex:   regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
			replace: emailLast4,
		},
		{
			id:    "ssh-key-path",
			regex: regexp.MustCompile(`(?:^|\s|"|')~?/?(?:[\w/\-]*?)?\.ssh/[\w./\-]+`),
			replace: func(string) string {
				return "[ssh-key-path-redacted]"
			},
		},
		{
			id:    "shadow-file-path",
			regex: regexp.MustCompile(`/etc/shadow\b`),
			replace: func(string) string {
				return "[shadow-path-redacted]"
			},
		},
		{
			id:    "macos-private-db-path",
			regex: regexp.MustCompile(`/private/var/db/[\w./\-]+`),
			replace: func(string) string {
				return "[macos-private-db-path-redacted]"
			},
		},
		{
			id:    "oracle-profile-path",
			regex: regexp.MustCompile(`(?:~|/Users/[^/\s]+|/home/[^/\s]+)/(?:\.oracle|\.config/oracle|Library/Application Support/oracle)[^\s"']*`),
			replace: func(string) string {
				return "[oracle-profile-path-redacted]"
			},
		},
		{
			id:    "user-home-path",
			regex: regexp.MustCompile(`(?:~|/Users/[^/\s"']+|/home/[^/\s"']+)/(?:[^\s"']+)`),
			replace: func(string) string {
				return "[user-path-redacted]"
			},
		},
		{
			id:    "vps-project-path",
			regex: regexp.MustCompile(`/data/projects/[^\s"']+`),
			replace: func(string) string {
				return "[project-path-redacted]"
			},
		},
	}
}

func hashTag(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:4])
}

func providerKeyReplace(prefix string) func(string) string {
	return func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return prefix + "[redacted-sha256:" + hex.EncodeToString(sum[:4]) + "]"
	}
}

func bearerReplace(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "Bearer [redacted-sha256:" + hex.EncodeToString(sum[:4]) + "]"
}

func emailLast4(s string) string {
	at := strings.LastIndex(s, "@")
	if at <= 0 {
		return s
	}
	local := s[:at]
	domain := s[at:]
	keep := 4
	if len(local) < keep {
		keep = len(local)
	}
	return "***" + local[len(local)-keep:] + domain
}
