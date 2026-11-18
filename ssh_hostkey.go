// Build tag note: tagged go1.11 like transfer_sftp.go (x/crypto is not
// available to the Go 1.10 XP toolchain).

//go:build go1.11
// +build go1.11

package main

import (
	"crypto/ed25519"
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

// knownHostKeyAlgorithms returns the host key algorithms matching the key
// types already stored for hostname, or nil if none are known. The SSH client
// must negotiate one of these: otherwise a server with several host keys
// (ed25519, ecdsa, rsa) could present a different *type* than the one trusted
// on first use, and that key would be refused as "changed" (OpenSSH restricts
// its algorithms the same way).
func knownHostKeyAlgorithms(path, hostname string) []string {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil
	}
	// Probe with a key that is never stored: the KeyError lists every known
	// key of the host.
	probe, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public())
	if err != nil {
		return nil
	}
	keyErr, ok := cb(hostname, &net.TCPAddr{}, probe).(*knownhosts.KeyError)
	if !ok {
		return nil
	}
	var algos []string
	seen := map[string]bool{}
	add := func(a string) {
		if !seen[a] {
			seen[a] = true
			algos = append(algos, a)
		}
	}
	for _, k := range keyErr.Want {
		switch t := k.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			add(ssh.KeyAlgoRSASHA512)
			add(ssh.KeyAlgoRSASHA256)
			add(ssh.KeyAlgoRSA)
		default:
			add(t)
		}
	}
	return algos
}

// hostKeyAlgoGroups are the host key algorithm groups tried, one by one, when
// a pinned fingerprint does not name its key type. A server has one key per
// type and only presents the type negotiated in the handshake, so a pinned
// ed25519 fingerprint can only match if ed25519 is negotiated.
var hostKeyAlgoGroups = [][]string{
	{ssh.KeyAlgoED25519},
	{ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521},
	{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
}

// pinnedHostKeyAlgorithms returns the algorithms for the key type named in a
// pinned fingerprint (WinSCP form "ssh-ed25519 255 SHA256:..."), or nil if the
// pin does not name a type (bare "SHA256:..." or MD5).
func pinnedHostKeyAlgorithms(pinned string) []string {
	for _, tok := range strings.Fields(pinned) {
		switch tok {
		case ssh.KeyAlgoED25519:
			return []string{ssh.KeyAlgoED25519}
		case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
			return []string{tok}
		case ssh.KeyAlgoRSA, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512:
			return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
		}
	}
	return nil
}
