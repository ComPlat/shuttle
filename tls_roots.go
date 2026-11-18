//go:build go1.11
// +build go1.11

package main

// legacyTLSRoots is false: Go 1.11+ can read the Windows root store.
const legacyTLSRoots = false
