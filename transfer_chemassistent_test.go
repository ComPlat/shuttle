package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const camTestKey = "cam_secretkey123"

type camUpload struct {
	names    []string
	contents []string
	message  string
	length   int64
	got      int
}

// camStub implements the server contract.
type camStub struct {
	mu       sync.Mutex
	srv      *httptest.Server
	posts    []camUpload
	gets     int
	status   int    // 0 = 202
	errText  string // error text for non-202
	conns    map[string]bool
	violated []string
}

func newCamStub() *camStub {
	s := &camStub{conns: make(map[string]bool)}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *camStub) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[r.RemoteAddr] = true
	if r.Header.Get("Authorization") != "Bearer "+camTestKey {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"status":"error","error":"bad key `+r.Header.Get("Authorization")+`"}`)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/jobs/runs/0" {
		s.gets++
		w.WriteHeader(404)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/api/jobs/trigger" {
		w.WriteHeader(404)
		return
	}
	if r.ContentLength <= 0 || len(r.TransferEncoding) != 0 {
		s.violated = append(s.violated, "chunked or no content length")
		w.WriteHeader(400)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		w.WriteHeader(400)
		return
	}
	up := camUpload{length: r.ContentLength, message: r.FormValue("message")}
	for _, fh := range r.MultipartForm.File["file"] {
		f, _ := fh.Open()
		data, _ := ioutil.ReadAll(f)
		f.Close()
		up.names = append(up.names, fh.Filename)
		up.contents = append(up.contents, string(data))
		up.got += len(data)
	}
	s.posts = append(s.posts, up)
	if s.status != 0 {
		w.WriteHeader(s.status)
		fmt.Fprint(w, `{"status":"error","error":"`+s.errText+`"}`)
		return
	}
	w.WriteHeader(202)
	fmt.Fprint(w, `{"run": 1, "status": "queued"}`)
}

func (s *camStub) manager(t *testing.T, key string, path string, maxMB int64) *TransferManagerChemAssistent {
	u, err := url.Parse(s.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return &TransferManagerChemAssistent{
		args:            &Args{name: "inst", maxUploadMB: maxMB, sendType: "file"},
		DestCredentials: DestCredentials{pass: key, dst: *u, srcDir: TempPath},
	}
}

func TestCamSingleFile(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	p := filepath.Join(TempPath, "sub", "a b.csv")
	writeTestFile(t, p, "1,2,3\n")
	ok, err := m.send_file(p, "sub/a b.csv")
	if !ok || err != nil {
		t.Fatalf("send_file = %v, %v", ok, err)
	}
	if len(s.posts) != 1 || s.posts[0].names[0] != "sub__a b.csv" || s.posts[0].contents[0] != "1,2,3\n" {
		t.Fatalf("unexpected upload %+v", s.posts)
	}
	if !strings.Contains(s.posts[0].message, "sub/a b.csv") || !strings.Contains(s.posts[0].message, "inst") {
		t.Errorf("message = %q", s.posts[0].message)
	}
	if len(s.violated) != 0 {
		t.Error(s.violated)
	}
}

func TestCamEmptyFile(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	p := filepath.Join(TempPath, "empty.txt")
	writeTestFile(t, p, "")
	if ok, err := m.send_file(p, "empty.txt"); !ok || err != nil {
		t.Fatalf("send_file = %v, %v", ok, err)
	}
	if len(s.posts) != 1 || s.posts[0].contents[0] != "" || len(s.posts[0].names) != 1 {
		t.Fatalf("unexpected upload %+v", s.posts)
	}
}

func TestCamLargeFileLength(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	p := filepath.Join(TempPath, "big.bin")
	writeTestFile(t, p, strings.Repeat("0123456789abcdef", 1<<16))
	if ok, err := m.send_file(p, "big.bin"); !ok || err != nil {
		t.Fatalf("send_file = %v, %v", ok, err)
	}
	if s.posts[0].got != 1<<20 || len(s.violated) != 0 {
		t.Fatalf("got %d bytes, violations %v", s.posts[0].got, s.violated)
	}
	if s.posts[0].length <= int64(s.posts[0].got) {
		t.Error("content length does not include multipart framing")
	}
}

func TestCamErrorStatuses(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	p := filepath.Join(TempPath, "a.txt")
	writeTestFile(t, p, "data")
	for _, code := range []int{409, 413} {
		s.status = code
		s.errText = "server says no"
		m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
		ok, err := m.send_file(p, "a.txt")
		if ok || err == nil || !strings.Contains(err.Error(), "server says no") || !strings.Contains(err.Error(), fmt.Sprint(code)) {
			t.Errorf("status %d: ok=%v err=%v", code, ok, err)
		}
	}
	// 401: the stub echoes the Authorization header; the key must be redacted.
	m := s.manager(t, "cam_wrong", "/api/jobs/trigger", 0)
	ok, err := m.send_file(p, "a.txt")
	if ok || err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401: ok=%v err=%v", ok, err)
	}
	if strings.Contains(err.Error(), "cam_wrong") {
		t.Errorf("error leaks the key: %v", err)
	}
	if !exists(p) {
		t.Error("local file removed")
	}
}

func TestCamKeyRedactedInNetworkError(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	m.dst.Path = "/api/jobs/trigger"
	p := filepath.Join(TempPath, "a.txt")
	writeTestFile(t, p, "data")
	s.srv.Close()
	ok, err := m.send_file(p, "a.txt")
	if ok || err == nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if strings.Contains(err.Error(), camTestKey) {
		t.Error("error leaks the key")
	}
	if m.client != nil {
		t.Error("client was not reset after a network error")
	}
}

func TestCamOverLimit(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 1)
	p := filepath.Join(TempPath, "big.bin")
	writeTestFile(t, p, strings.Repeat("x", (1<<20)+1))
	ok, err := m.send_file(p, "big.bin")
	if ok || err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(s.posts) != 0 || len(s.conns) != 0 {
		t.Error("a request was sent")
	}
}

func TestCamConnect(t *testing.T) {
	s := newCamStub()
	defer s.srv.Close()
	if err := s.manager(t, camTestKey, "/api/jobs/trigger", 0).connect_to_server(); err != nil {
		t.Errorf("valid key: %v", err)
	}
	if s.gets != 1 {
		t.Errorf("gets = %d", s.gets)
	}
	err := s.manager(t, "cam_bad", "/api/jobs/trigger", 0).connect_to_server()
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("invalid key: %v", err)
	}
	if err := s.manager(t, camTestKey, "/custom/trigger", 0).connect_to_server(); err != nil {
		t.Errorf("custom path: %v", err)
	}
	if s.gets != 1 {
		t.Errorf("custom path sent a GET (%d)", s.gets)
	}
	s.srv.Close()
	if err := s.manager(t, camTestKey, "/api/jobs/trigger", 0).connect_to_server(); err == nil {
		t.Error("expected a network error")
	}
}

func TestCamClientReuse(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	p := filepath.Join(TempPath, "a.txt")
	writeTestFile(t, p, "data")
	for i := 0; i < 2; i++ {
		if ok, err := m.send_file(p, "a.txt"); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	c := m.client
	if c == nil || len(s.conns) != 1 {
		t.Errorf("client %v, connections %d", c, len(s.conns))
	}
	if _, err := m.send_file(p, "a.txt"); err != nil || m.client != c {
		t.Error("client was replaced")
	}
}

func TestCamFolderCycle(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	writeTestFile(t, filepath.Join(TempPath, "A", "B", "x.csv"), "bx")
	writeTestFile(t, filepath.Join(TempPath, "A", "y.csv"), "y")
	writeTestFile(t, filepath.Join(TempPath, "A", "C", "x.csv"), "cx")
	writeTestFile(t, filepath.Join(TempPath, "A", "B__x.csv"), "clash")

	s.status = 409
	s.errText = "disabled"
	if n := transferCycle(m, TempPath, "folder", 0); n != 1 {
		t.Fatalf("failures = %d", n)
	}
	if !exists(filepath.Join(TempPath, "A", "y.csv")) {
		t.Fatal("folder removed after failure")
	}
	s.status = 0
	s.posts = nil
	if n := transferCycle(m, TempPath, "folder", 0); n != 0 {
		t.Fatalf("failures = %d", n)
	}
	if len(s.posts) != 1 || len(s.posts[0].names) != 4 {
		t.Fatalf("posts = %+v", s.posts)
	}
	seen := make(map[string]bool)
	var all bytes.Buffer
	for i, n := range s.posts[0].names {
		if seen[n] || strings.Contains(n, "/") {
			t.Errorf("bad name %q", n)
		}
		seen[n] = true
		all.WriteString(n + "=" + s.posts[0].contents[i] + ";")
	}
	if !seen["A__B__x.csv"] || !seen["A__B__x_2.csv"] || !seen["A__C__x.csv"] || !seen["A__y.csv"] || !strings.Contains(s.posts[0].message, "folder A") {
		t.Errorf("names %v message %q", all.String(), s.posts[0].message)
	}
	if exists(filepath.Join(TempPath, "A")) {
		t.Error("folder kept after success")
	}
}

func TestCamFileModeOneRequestPerFile(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	s := newCamStub()
	defer s.srv.Close()
	m := s.manager(t, camTestKey, "/api/jobs/trigger", 0)
	writeTestFile(t, filepath.Join(TempPath, "A", "x.csv"), "x")
	writeTestFile(t, filepath.Join(TempPath, "A", "y.csv"), "y")
	if n := transferCycle(m, TempPath, "file", 0); n != 0 {
		t.Fatalf("failures = %d", n)
	}
	if len(s.posts) != 2 {
		t.Fatalf("posts = %d", len(s.posts))
	}
}

func TestFlattenAndUniqueNames(t *testing.T) {
	if got := flattenName("A/B/x\"\n.csv"); got != "A__B__x__.csv" {
		t.Errorf("flattenName = %q", got)
	}
	got := uniqueNames([]string{"a.csv", "a.csv", "a.csv", "b"})
	want := []string{"a.csv", "a_2.csv", "a_3.csv", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("uniqueNames = %v", got)
		}
	}
}
