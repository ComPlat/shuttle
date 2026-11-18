//go:build !go1.11
// +build !go1.11

package main

// legacyTLSRoots is true: Go 1.10 cannot read the Windows root store, so
// verification needs -crt.
const legacyTLSRoots = true
