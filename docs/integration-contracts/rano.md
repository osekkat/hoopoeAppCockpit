# `rano` integration contract

> Diagnostics-only per-call latency and error observations for subscription-backed AI CLIs. `caut` remains the subscription-quota source of truth; `rano` does not feed the v1 top bar or `watch-safety-thresholds`.

## Source of truth

| Field | Value |
| ----- | ----- |
| Tool | `rano` |
| Repo | `github.com/Dicklesworthstone/rano` |
| Min compatible | `0.1.0` |
| Capability | `rano.signals.read` |
| v1 surface | Diagnostics "Recent AI CLI calls" only |

## Adopted command surfaces

| Label | argv | Exit | Notes |
| ----- | ---- | ---- | ----- |
| Version | `rano --version` | 0 | Probe identity. Accepts `rano version X.Y.Z`, `rano X.Y.Z`, or `vX.Y.Z`. |
| Signals | `rano signals --json --since <duration>` | 0 | Primary read for recent per-call observations. Duration uses Go-style units such as `15m` or `90s`. |

No shell execution is used. The adapter constructs argv directly and rejects unadopted surfaces.

## Signals JSON

```json
{
  "generated_at": "2026-05-04T00:15:00Z",
  "rano_version": "0.2.1",
  "window": {
    "start": "2026-05-04T00:00:00Z",
    "end": "2026-05-04T00:15:00Z"
  },
  "observations": [
    {
      "timestamp": "2026-05-04T00:03:00Z",
      "harness": "claude",
      "model": "claude-opus",
      "latency_ms": 900,
      "status": "error",
      "error_class": "rate_limit",
      "http_status": 429,
      "endpoint_redacted": "https://api.anthropic.com/[redacted]"
    }
  ]
}
```

`calls` is accepted as a legacy alias for `observations`. If `summary` or `by_harness_model` are absent, Hoopoe computes them from observations:

| Field | Meaning |
| ----- | ------- |
| `total_calls` | Count of observations in the window. |
| `error_count` | Observations with `status != "ok"`, non-empty `error_class`, or HTTP status >= 400. |
| `latency_p50_ms`, `latency_p95_ms` | Nearest-rank percentiles over `latency_ms`. |
| `last_error_class` | Latest observed explicit `error_class`, or `http_<status>` if only HTTP status is known. |

## Redaction posture

The adapter accepts redacted endpoint metadata only. It rejects observations containing raw payload fields: `body`, `raw_body`, `request_body`, `response_body`, `payload`, or `raw_payload`.

## Capability semantics

| capId | Status rules |
| ----- | ------------ |
| `rano._present` | `ok` when `rano --version` succeeds and version is compatible; `missing` when binary is absent; `degraded` when version is unsupported. |
| `rano.signals.read` | `ok` when `rano signals --json --since 15m` parses; `degraded` for malformed JSON, timeout/non-zero exit, redaction-contract violation, or output over the daemon limit. |

## Explicit v1 non-goals

- Not wired into the top-bar subscription pill. That remains `caut`.
- Not wired into `watch-safety-thresholds`. v1 uses `caut`, CLI status messages, NTM events, and CAAM.
- Not an interceptor. `rano` is treated as an observer whose output is already redacted.
