package main

// startupChecks verifies, once at startup, everything the background workers
// need and cannot recover from at runtime (e.g. the converter executable when
// -preconvert is set, a readable scripts folder). main stops with a clear
// message if it returns an error. Filled in by WP 7.2.
func startupChecks(a *Args) error {
	return nil
}
