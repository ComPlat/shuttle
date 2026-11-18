package main

import "fmt"

// startupChecks verifies, once at startup, everything the background workers
// need and cannot recover from at runtime: a readable scripts folder and,
// with -preconvert, the converter executable. main stops with a clear
// message if it returns an error.
func startupChecks(a *Args) error {
	if _, err := ReadDirCompat(PreScriptPath); err != nil {
		return fmt.Errorf("scripts folder %s is not readable: %v", PreScriptPath, err)
	}
	if a != nil && a.preconvert {
		if _, err := converterPath(); err != nil {
			return err
		}
	}
	return nil
}
