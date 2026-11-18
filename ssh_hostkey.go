// Build tag note: tagged go1.11 like transfer_sftp.go (x/crypto is not
// available to the Go 1.10 XP toolchain).

//go:build go1.11
// +build go1.11

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	// knownHostsMu protects the read-modify-append of the known_hosts file.
	knownHostsMu sync.Mutex
	// insecureHostKeyOnce makes the -insecure warning appear once.
	insecureHostKeyOnce sync.Once
)

// hostKeyCallback returns the SSH host key policy: -insecure accepts every
// key, a pinned fingerprint (-hostkey) must match, otherwise the key is
// trusted on first use and stored in knownHostsPath.
func hostKeyCallback(pinned string, insecure bool, knownHostsPath string) (ssh.HostKeyCallback, error) {
	if insecure {
		insecureHostKeyOnce.Do(func() {
			if ErrorLogger != nil {
				ErrorLogger.Println("SSH host key verification is DISABLED (-insecure)")
			}
		})
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }, nil
	}
	pinned = strings.TrimSpace(pinned)
	if pinned != "" {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if fingerprintMatches(pinned, key) {
				return nil
			}
			return fmt.Errorf("SSH host key mismatch for %s: expected %q, server presented %s",
				hostname, pinned, ssh.FingerprintSHA256(key))
		}, nil
	}
	if knownHostsPath == "" {
		return nil, errors.New("no known_hosts path for SSH host key verification")
	}
	// make sure the file exists and parses
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	if _, err := newKnownHosts(knownHostsPath); err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return tofuCheck(knownHostsPath, hostname, remote, key)
	}, nil
}

// newKnownHosts creates the file if needed (empty, 0600) and parses it.
// The caller holds knownHostsMu.
func newKnownHosts(path string) (ssh.HostKeyCallback, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot create known_hosts file %s: %v", path, err)
	}
	_ = f.Close()
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read known_hosts file %s: %v", path, err)
	}
	return cb, nil
}

func tofuCheck(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()

	// Rebuild from the file every time, so keys stored by other managers
	// (or a previous run) are seen.
	cb, err := newKnownHosts(path)
	if err != nil {
		return err
	}
	err = cb(hostname, remote, key)
	if err == nil {
		return nil
	}
	keyErr, ok := err.(*knownhosts.KeyError)
	if !ok {
		return err
	}
	if len(keyErr.Want) > 0 {
		msg := fmt.Sprintf("SSH host key for %s CHANGED (presented %s). If the server key was changed on purpose, remove the line for %s from %s or set -hostkey",
			hostname, ssh.FingerprintSHA256(key), hostname, path)
		if ErrorLogger != nil {
			ErrorLogger.Println(msg)
		}
		return errors.New(msg)
	}
	// unknown host: trust on first use
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("cannot store SSH host key in %s: %v", path, err)
	}
	_, werr := f.WriteString(line + "\n")
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("cannot store SSH host key in %s: %v", path, werr)
	}
	if cerr != nil {
		return fmt.Errorf("cannot store SSH host key in %s: %v", path, cerr)
	}
	if InfoLogger != nil {
		InfoLogger.Printf("trusting new SSH host key %s for %s (stored in %s)", ssh.FingerprintSHA256(key), hostname, path)
	}
	return nil
}

// fingerprintMatches reports whether key matches a pinned fingerprint:
// "SHA256:<base64>", WinSCP style "ssh-ed25519 255 SHA256:<base64>" or
// legacy MD5 "aa:bb:..." (optionally prefixed "MD5:").
func fingerprintMatches(pinned string, key ssh.PublicKey) bool {
	pinned = strings.TrimSpace(pinned)
	if pinned == "" || key == nil {
		return false
	}
	for _, tok := range strings.Fields(pinned) {
		if strings.HasPrefix(tok, "SHA256:") {
			return tok == ssh.FingerprintSHA256(key)
		}
	}
	md5 := strings.ToLower(strings.TrimPrefix(pinned, "MD5:"))
	return md5 == strings.TrimPrefix(ssh.FingerprintLegacyMD5(key), "MD5:")
}
