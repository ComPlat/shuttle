package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type putRecord struct {
	path             string
	contentLength    int64
	transferEncoding []string
	body             []byte
}

// davStub is a minimal WebDAV server: OPTIONS (used by Connect), MKCOL and PUT.
type davStub struct {
	mu      sync.Mutex
	srv     *httptest.Server
	puts    []putRecord
	options int
	putCode int
}

func newDavStub(putCode int) *davStub {
	d := &davStub{putCode: putCode}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		switch r.Method {
		case "OPTIONS":
			d.options++
			w.Header().Set("DAV", "1")
			w.WriteHeader(200)
		case "MKCOL":
			w.WriteHeader(201)
		case "PUT":
			body, _ := ioutil.ReadAll(r.Body)
			d.puts = append(d.puts, putRecord{r.URL.Path, r.ContentLength, r.TransferEncoding, body})
			w.WriteHeader(d.putCode)
		default:
			w.WriteHeader(405)
		}
	}))
	return d
}

func (d *davStub) manager(t *testing.T) *TransferManagerWebdav {
	u, err := url.Parse(d.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &TransferManagerWebdav{
		DestCredentials: DestCredentials{user: "u", pass: "p", dst: *u},
		args:            &Args{},
	}
}

func useTestTransport() func() {
	old := tr
	tr = newTransport(nil)
	return func() {
		tr.CloseIdleConnections()
		tr = old
	}
}

func TestWebdavPutIsSizedNotChunked(t *testing.T) {
	defer useTestTransport()()
	dir, cleanup := newTempDir(t)
	defer cleanup()
	d := newDavStub(201)
	defer d.srv.Close()

	data := bytes.Repeat([]byte("0123456789abcdef"), 1048576/16)
	local := filepath.Join(dir, "big.bin")
	writeTestFile(t, local, string(data))

	m := d.manager(t)
	ok, err := m.send_file(local, "sub/big.bin")
	if !ok || err != nil {
		t.Fatalf("send_file = %v, %v", ok, err)
	}
	if len(d.puts) != 1 {
		t.Fatalf("puts = %d", len(d.puts))
	}
	p := d.puts[0]
	if p.path != "/sub/big.bin" {
		t.Errorf("path = %q", p.path)
	}
	if p.contentLength != 1048576 {
		t.Errorf("ContentLength = %d, want 1048576", p.contentLength)
	}
	if len(p.transferEncoding) != 0 {
		t.Errorf("chunked upload: %v", p.transferEncoding)
	}
	if !bytes.Equal(p.body, data) {
		t.Error("body differs")
	}
}

func TestWebdavEmptyFile(t *testing.T) {
	defer useTestTransport()()
	dir, cleanup := newTempDir(t)
	defer cleanup()
	d := newDavStub(201)
	defer d.srv.Close()
	local := filepath.Join(dir, "empty")
	writeTestFile(t, local, "")
	ok, err := d.manager(t).send_file(local, "empty")
	if !ok || err != nil {
		t.Fatalf("send_file = %v, %v", ok, err)
	}
	p := d.puts[0]
	if p.contentLength != 0 || len(p.transferEncoding) != 0 || len(p.body) != 0 {
		t.Errorf("empty put = %+v", p)
	}
}

func TestWebdavPutFailure(t *testing.T) {
	defer useTestTransport()()
	dir, cleanup := newTempDir(t)
	defer cleanup()
	d := newDavStub(500)
	defer d.srv.Close()
	local := filepath.Join(dir, "a.txt")
	writeTestFile(t, local, "a")
	m := d.manager(t)
	if ok, err := m.send_file(local, "a.txt"); ok || err == nil {
		t.Fatalf("send_file = %v, %v, want failure", ok, err)
	}
	if m.client != nil {
		t.Error("client kept after an error")
	}
}

func TestWebdavMissingLocalFile(t *testing.T) {
	defer useTestTransport()()
	d := newDavStub(201)
	defer d.srv.Close()
	m := d.manager(t)
	if ok, err := m.send_file(filepath.Join(d.srv.URL, "nope"), "x"); ok || err == nil {
		t.Fatal("expected failure")
	}
}

func TestWebdavConnectionReuse(t *testing.T) {
	defer useTestTransport()()
	dir, cleanup := newTempDir(t)
	defer cleanup()
	d := newDavStub(201)
	defer d.srv.Close()
	m := d.manager(t)
	for i := 0; i < 5; i++ {
		local := filepath.Join(dir, fmt.Sprintf("f%d", i))
		writeTestFile(t, local, "x")
		if ok, err := m.send_file(local, fmt.Sprintf("f%d", i)); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if d.options != 1 {
		t.Errorf("connects = %d, want 1 for 5 files", d.options)
	}
	// After an error the client is recreated.
	d.mu.Lock()
	d.putCode = 500
	d.mu.Unlock()
	local := filepath.Join(dir, "bad")
	writeTestFile(t, local, "x")
	if ok, _ := m.send_file(local, "bad"); ok {
		t.Fatal("expected failure")
	}
	d.mu.Lock()
	d.putCode = 201
	d.mu.Unlock()
	if ok, err := m.send_file(local, "bad"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if d.options != 2 {
		t.Errorf("connects = %d, want 2 after one error", d.options)
	}
}

func TestWebdavClientHasNoOverallTimeout(t *testing.T) {
	defer useTestTransport()()
	d := newDavStub(201)
	defer d.srv.Close()
	m := d.manager(t)
	if err := m.connect_to_server(); err != nil {
		t.Fatal(err)
	}
	// The fork's http.Client is unexported; read its Timeout by reflection.
	hc := reflect.ValueOf(m.client).Elem().FieldByName("c").Elem()
	if got := time.Duration(hc.FieldByName("Timeout").Int()); got != 0 {
		t.Errorf("client Timeout = %v, want none", got)
	}
}

func TestNewTransportTimeouts(t *testing.T) {
	x := newTransport(nil)
	if x.Proxy == nil {
		t.Error("no proxy from environment")
	}
	if x.DialContext == nil || x.TLSHandshakeTimeout != 30*time.Second ||
		x.ResponseHeaderTimeout != 5*time.Minute || x.IdleConnTimeout != 90*time.Second {
		t.Errorf("transport timeouts not configured: %+v", x)
	}
}

func TestSizedTransportKeepsOtherRequests(t *testing.T) {
	var got int64 = -2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.ContentLength
	}))
	defer srv.Close()
	c := &http.Client{Transport: &sizedTransport{}}
	resp, err := c.Post(srv.URL, "text/plain", strings.NewReader("abc"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != 3 {
		t.Errorf("ContentLength = %d", got)
	}
}
