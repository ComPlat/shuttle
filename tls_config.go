package main

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
)

// buildTLSConfig returns the TLS config for WebDAV and the warnings to log.
// crtPath must already be placeholder-cleaned. legacyRoots is true on the
// Windows XP build, where the system root store cannot be read: there
// verification is only switched on when a usable crtPath is given.
func buildTLSConfig(crtPath string, insecure, legacyRoots bool, systemPool func() (*x509.CertPool, error)) (*tls.Config, []string) {
	var warnings []string
	if insecure {
		warnings = append(warnings, "TLS certificate verification is DISABLED (-insecure)")
		return &tls.Config{InsecureSkipVerify: true}, warnings
	}

	var pool *x509.CertPool
	if systemPool != nil {
		p, err := systemPool()
		if err != nil || p == nil {
			if !legacyRoots {
				if err != nil {
					warnings = append(warnings, "cannot load the system certificate pool, using only -crt: "+err.Error())
				} else {
					warnings = append(warnings, "cannot load the system certificate pool, using only -crt")
				}
			}
		} else {
			pool = p
		}
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}

	crtOK := false
	if crtPath != "" {
		data, err := ReadFileCompat(crtPath)
		if err != nil {
			warnings = append(warnings, "cannot read -crt file "+crtPath+": "+err.Error())
		} else if !pool.AppendCertsFromPEM(data) {
			warnings = append(warnings, "no certificate found in -crt file "+crtPath)
		} else {
			crtOK = true
		}
	}

	if legacyRoots && !crtOK {
		warnings = append(warnings, "Windows XP build without -crt: TLS certificate verification is DISABLED. Pass the server certificate with -crt to enable it.")
		return &tls.Config{InsecureSkipVerify: true, RootCAs: pool}, warnings
	}
	return &tls.Config{InsecureSkipVerify: false, RootCAs: pool}, warnings
}

// resolveCrtPath returns the -crt file to read. An empty or absolute path is
// returned unchanged. A relative path is looked up in exeDir (the folder of
// the executable) first, because the start files (Windows .vbs, systemd unit)
// don't set a working directory; if it isn't there, the path stays relative to
// the working directory as before.
func resolveCrtPath(p, exeDir string) string {
	if p == "" || filepath.IsAbs(p) || exeDir == "" {
		return p
	}
	candidate := filepath.Join(exeDir, p)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return p
}

// executableDir returns the folder of the running executable, or "" if it
// cannot be determined.
func executableDir() string {
	ex, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(ex)
}
