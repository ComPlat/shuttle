//go:build go1.16
// +build go1.16

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func testKnownHosts(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "hostkey")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "known_hosts")
}

var (
	testAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	testHost = "sftp.example:2222"
)

func TestHostKeyTOFU(t *testing.T) {
	path := testKnownHosts(t)
	k1, k2 := testKey(t), testKey(t)
	cb, err := hostKeyCallback("", false, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(testHost, testAddr, k1); err != nil {
		t.Fatalf("first key: %v", err)
	}
	if err := cb(testHost, testAddr, k1); err != nil {
		t.Fatalf("same key again: %v", err)
	}
	// restart: new callback, same file
	cb2, err := hostKeyCallback("", false, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb2(testHost, testAddr, k1); err != nil {
		t.Fatalf("after restart: %v", err)
	}
	err = cb2(testHost, testAddr, k2)
	if err == nil {
		t.Fatal("different key was accepted")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the known_hosts file: %v", err)
	}
	// old callback also refuses
	if err := cb(testHost, testAddr, k2); err == nil {
		t.Fatal("different key accepted by old callback")
	}
	// second host gets its own line
	if err := cb("other.example:2222", testAddr, k2); err != nil {
		t.Fatalf("second host: %v", err)
	}
	data, _ := ioutil.ReadFile(path)
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 2 {
		t.Errorf("want 2 lines in known_hosts, got %d:\n%s", n, data)
	}
}

func TestHostKeyFilePerm(t *testing.T) {
	path := testKnownHosts(t)
	if _, err := hostKeyCallback("", false, path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Errorf("perm = %v, want 0600", st.Mode().Perm())
	}
}

func TestHostKeyPinned(t *testing.T) {
	k, other := testKey(t), testKey(t)
	sha := ssh.FingerprintSHA256(k)
	for _, pin := range []string{sha, "ssh-ed25519 255 " + sha, ssh.FingerprintLegacyMD5(k), strings.TrimPrefix(ssh.FingerprintLegacyMD5(k), "MD5:")} {
		cb, err := hostKeyCallback(pin, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := cb(testHost, testAddr, k); err != nil {
			t.Errorf("pin %q refused: %v", pin, err)
		}
	}
	cb, _ := hostKeyCallback(sha, false, "")
	err := cb(testHost, testAddr, other)
	if err == nil {
		t.Fatal("wrong key accepted")
	}
	if !strings.Contains(err.Error(), sha) || !strings.Contains(err.Error(), ssh.FingerprintSHA256(other)) {
		t.Errorf("error lacks fingerprints: %v", err)
	}
}

func TestHostKeyInsecure(t *testing.T) {
	path := testKnownHosts(t)
	cb, err := hostKeyCallback("SHA256:whatever", true, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(testHost, testAddr, testKey(t)); err != nil {
		t.Fatal(err)
	}
	if err := cb(testHost, testAddr, testKey(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("known_hosts must not be created with -insecure: %v", err)
	}
}

func TestFingerprintMatches(t *testing.T) {
	k, other := testKey(t), testKey(t)
	sha := ssh.FingerprintSHA256(k)
	md5 := ssh.FingerprintLegacyMD5(k)
	cases := []struct {
		pin  string
		want bool
	}{
		{sha, true},
		{" " + sha + " ", true},
		{"ssh-ed25519 255 " + sha, true},
		{md5, true},
		{strings.TrimPrefix(md5, "MD5:"), true},
		{ssh.FingerprintSHA256(other), false},
		{ssh.FingerprintLegacyMD5(other), false},
		{strings.ToLower(sha), false},
		{"SHA256:", false},
		{"", false},
		{"garbage", false},
	}
	for _, c := range cases {
		if got := fingerprintMatches(c.pin, k); got != c.want {
			t.Errorf("fingerprintMatches(%q) = %v, want %v", c.pin, got, c.want)
		}
	}
}
