// Package main hosts the Hoopoe Go daemon entry point. The full daemon
// (HTTP/WS scaffolding, auth, event stream, adapters, tending scheduler) is
// built across Phase 2+ beads. For hp-xru this file is a smoke entry so
// `go build ./...` succeeds and the binary name is reserved.
package main

import "fmt"

// Version is the daemon build version. Wired up properly via -ldflags in
// hp-191's release pipeline; for now this is a placeholder.
const Version = "0.0.0"

func main() {
	fmt.Printf("hoopoe daemon %s — Phase 2 not yet implemented\n", Version)
}
