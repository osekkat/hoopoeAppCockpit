// Package load houses Phase 2.5 bounded-channel + slow-consumer load
// tests (hp-q3p). Each test asserts §10.1 backpressure invariants
// + Appendix B anti-pattern #1 (PubSub.unbounded) compliance.
//
// All tests in this package skip under `go test -short ./...` so
// normal CI doesn't pay their cost. To run:
//
//   rch exec -- go test ./internal/transport/load/...
//
// Or, with a longer timeout, locally:
//
//   go test -timeout 5m ./internal/transport/load/...
package load
