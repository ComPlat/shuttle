package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func emptyPool() (*x509.CertPool, error) { return x509.NewCertPool(), nil }

func newTLSTestServer() *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
}

// newOtherTLSServer starts a TLS server with its own certificate (all
// httptest.NewTLSServer instances share one certificate).
func newOtherTLSServer(t *testing.T) *httptest.Server {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(4242),
		Subject:      pkix.Name{CommonName: "other"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	var pair tls.Certificate
	pair.Certificate = [][]byte{der}
	pair.PrivateKey = key
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	srv.StartTLS()
	return srv
}

func writeServerCrt(t *testing.T, dir string, srv *httptest.Server) string {
	p := filepath.Join(dir, "server.crt")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := ioutil.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func tlsGet(cfgSrc func() *http.Transport, url string) error {
	c := &http.Client{Transport: cfgSrc()}
	resp, err := c.Get(url)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func tmpDirTLS(t *testing.T) (string, func()) {
	d, err := ioutil.TempDir("", "tlscfg")
	if err != nil {
		t.Fatal(err)
	}
	return d, func() { os.RemoveAll(d) }
}

func get(t *testing.T, crt string, insecure, legacy bool, pool func() (*x509.CertPool, error), url string) (error, []string) {
	cfg, warns := buildTLSConfig(crt, insecure, legacy, pool)
	return tlsGet(func() *http.Transport { return &http.Transport{TLSClientConfig: cfg} }, url), warns
}

func hasWarn(w []string, sub string) bool {
	for _, s := range w {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestTLSDefaultRejectsUntrusted(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	err, warns := get(t, "", false, false, emptyPool, srv.URL)
	if err == nil {
		t.Fatal("expected certificate error")
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings %v", warns)
	}
}

func TestTLSCrtTrusted(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	d, done := tmpDirTLS(t)
	defer done()
	crt := writeServerCrt(t, d, srv)
	err, warns := get(t, crt, false, false, emptyPool, srv.URL)
	if err != nil {
		t.Fatalf("request with -crt failed: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings %v", warns)
	}
}

func TestTLSInsecure(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	err, warns := get(t, "", true, false, emptyPool, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarn(warns, "DISABLED (-insecure)") {
		t.Errorf("missing warning: %v", warns)
	}
}

func TestTLSLegacyWithoutCrt(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	err, warns := get(t, "", false, true, emptyPool, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarn(warns, "Windows XP build without -crt") {
		t.Errorf("missing XP warning: %v", warns)
	}
}

func TestTLSLegacyWithCrtVerifies(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	other := newOtherTLSServer(t)
	defer other.Close()
	d, done := tmpDirTLS(t)
	defer done()
	crt := writeServerCrt(t, d, srv)
	if err, _ := get(t, crt, false, true, emptyPool, srv.URL); err != nil {
		t.Fatalf("trusted server failed: %v", err)
	}
	err, warns := get(t, crt, false, true, emptyPool, other.URL)
	if err == nil {
		t.Fatal("untrusted server must fail when verification is on")
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings %v", warns)
	}
}

func TestTLSUnreadableCrt(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	d, done := tmpDirTLS(t)
	defer done()
	missing := filepath.Join(d, "nope.crt")
	err, warns := get(t, missing, false, false, emptyPool, srv.URL)
	if err == nil {
		t.Fatal("verification must stay on")
	}
	if !hasWarn(warns, missing) {
		t.Errorf("warning must name the path: %v", warns)
	}
	// a file without any certificate
	bad := filepath.Join(d, "bad.crt")
	if e := ioutil.WriteFile(bad, []byte("not pem"), 0600); e != nil {
		t.Fatal(e)
	}
	err, warns = get(t, bad, false, false, emptyPool, srv.URL)
	if err == nil || !hasWarn(warns, bad) {
		t.Errorf("bad crt: err=%v warns=%v", err, warns)
	}
}

func TestTLSUnreadableCrtLegacyStaysInsecure(t *testing.T) {
	cfg, warns := buildTLSConfig("/nonexistent/x.crt", false, true, emptyPool)
	if !cfg.InsecureSkipVerify {
		t.Error("legacy without usable crt must skip verification")
	}
	if !hasWarn(warns, "Windows XP build without -crt") || !hasWarn(warns, "/nonexistent/x.crt") {
		t.Errorf("warnings: %v", warns)
	}
}

func TestTLSSystemPoolError(t *testing.T) {
	srv := newTLSTestServer()
	defer srv.Close()
	failing := func() (*x509.CertPool, error) { return nil, errors.New("boom") }
	err, warns := get(t, "", false, false, failing, srv.URL)
	if err == nil {
		t.Fatal("verification must stay on")
	}
	if !hasWarn(warns, "system certificate pool") {
		t.Errorf("missing warning: %v", warns)
	}
	// legacy: no pool warning, only the XP one
	_, warns = buildTLSConfig("", false, true, failing)
	if hasWarn(warns, "system certificate pool") {
		t.Errorf("legacy must not warn about the pool: %v", warns)
	}
}
