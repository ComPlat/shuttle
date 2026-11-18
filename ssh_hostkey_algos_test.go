//go:build go1.16
// +build go1.16

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestKnownHostKeyAlgorithms(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	path := filepath.Join(dir, "known_hosts")
	host := "sftp.example:2222"
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}

	if got := knownHostKeyAlgorithms(path, host); got != nil {
		t.Errorf("missing file: got %v, want nil", got)
	}

	cb, err := hostKeyCallback("", false, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := knownHostKeyAlgorithms(path, host); got != nil {
		t.Errorf("unknown host: got %v, want nil", got)
	}

	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edKey, err := ssh.NewPublicKey(edPub)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(host, addr, edKey); err != nil {
		t.Fatal(err)
	}
	if got, want := knownHostKeyAlgorithms(path, host), []string{ssh.KeyAlgoED25519}; !reflect.DeepEqual(got, want) {
		t.Errorf("after trusting ed25519: got %v, want %v", got, want)
	}
	if got := knownHostKeyAlgorithms(path, "other.example:22"); got != nil {
		t.Errorf("other host: got %v, want nil", got)
	}

	// A second key type stored for the same host (as an admin would add it)
	// widens the list; RSA maps to the SHA-2 signature algorithms too.
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := ssh.NewPublicKey(&rsaPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("rsa.example:22", addr, rsaKey); err != nil {
		t.Fatal(err)
	}
	want := []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	if got := knownHostKeyAlgorithms(path, "rsa.example:22"); !reflect.DeepEqual(got, want) {
		t.Errorf("rsa host: got %v, want %v", got, want)
	}
}

func TestPinnedHostKeyAlgorithms(t *testing.T) {
	cases := map[string][]string{
		"ssh-ed25519 255 SHA256:abc":       {ssh.KeyAlgoED25519},
		"ecdsa-sha2-nistp256 256 SHA256:x": {ssh.KeyAlgoECDSA256},
		"ssh-rsa 2048 SHA256:x":            {ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		"SHA256:abc":                       nil,
		"aa:bb:cc":                         nil,
		"":                                 nil,
	}
	for pin, want := range cases {
		if got := pinnedHostKeyAlgorithms(pin); !reflect.DeepEqual(got, want) {
			t.Errorf("pinnedHostKeyAlgorithms(%q) = %v, want %v", pin, got, want)
		}
	}
}
