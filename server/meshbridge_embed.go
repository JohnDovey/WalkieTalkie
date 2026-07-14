//go:build meshbridge_embed

// Package-level embed hook (opt-in build tag). Default Base Station builds
// do not include MeshBridge; run the separate meshbridge binary instead.
//
// Example:
//
//	go build -tags meshbridge_embed ./server
//
// When enabled, start MeshBridge's sync pipeline in a goroutine from main.
// Full wiring is intentionally deferred — keep MeshBridge a separate process
// until the Mac transports are battle-tested.
package main

func init() {
	// Reserved for importing github.com/JohnDovey/WalkieTalkie/meshbridge/...
}
