// Generated Go types for the Hoopoe daemon API (`packages/schemas/openapi.yaml`).
//
// Consumers (apps/daemon, future Go SDKs) import this module either via a
// `replace` directive in their go.mod pointing at the relative path, or via
// a top-level `go.work` file that adds this module to the workspace. See
// `packages/schemas/README.md` for the canonical wiring example.
//
// This module is types-only: no chi/echo server stubs, no client code. The
// daemon picks its own HTTP framework and binds typed handlers to these
// types itself. That keeps the schema package decoupled from the daemon's
// transport choices.
module github.com/hoopoe-cockpit/hoopoe/packages/schemas/go

go 1.26
