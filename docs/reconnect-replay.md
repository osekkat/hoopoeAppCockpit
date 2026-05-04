# Reconnect and Replay

This note records the Phase 2.5 reconnect contract for byte-addressable job
logs. Event replay uses the transport sequence cursor; job logs use persisted
byte offsets because terminal/build output can exceed the in-memory event ring.

## Job Log Offsets

Clients read log bytes with:

```http
GET /v1/jobs/{jobId}/log?offset=<bytes>&maxBytes=<n>
```

The response body is raw `application/octet-stream` data from the persisted log
file. A client should persist the returned `X-Log-Next-Offset` value after each
successful response. After a tunnel drop or renderer restart, it resumes by
requesting that offset.

The daemon sets:

- `Content-Range`: byte range returned, or `bytes */<total>` for an empty chunk.
- `X-Log-Total-Bytes`: total bytes currently persisted for the job.
- `X-Log-Final`: `true` when the job is terminal.
- `X-Log-Next-Offset`: offset to use for the next request.

`maxBytes` is bounded by the daemon so a reconnect cannot force an unbounded
read. If omitted, the daemon uses the default chunk size. The legacy `limit`
query parameter is accepted by the daemon handler for seed-contract callers, but
new clients should send `maxBytes`.

## Polling Active Logs

For an active job, an empty response with `X-Log-Final: false` means the client
is caught up, not that the stream is complete. The client keeps the same offset
and polls again after its normal backoff.

For a terminal job, `X-Log-Final: true` plus an empty response at the current
offset means all persisted bytes have been observed.
