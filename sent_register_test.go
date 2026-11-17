package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// regFile creates a file with a fixed mtime and returns its FileInfo.
func regFile(t *testing.T, path, content string, mtime time.Time) os.FileInfo {
	t.Helper()
	writeTestFile(t, path, content)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestSentRegisterLoadMissing(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	r := LoadSentRegister(filepath.Join(dir, "sent_register.json"))
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
}

func TestSentRegisterRoundTrip(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	mt := time.Unix(1600000000, 123456789)
	a := regFile(t, filepath.Join(dir, "a.txt"), "aaa", mt)
	b := regFile(t, filepath.Join(dir, "b.txt"), "bb", mt)
	regPath := filepath.Join(dir, "sent_register.json")

	r := LoadSentRegister(regPath)
	r.Record("a.txt", a)
	r.Record("sub/b.txt", b)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r2 := LoadSentRegister(regPath)
	if r2.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r2.Len())
	}
	if !r2.Unchanged("a.txt", a) || !r2.Unchanged("sub/b.txt", b) {
		t.Error("entries lost in the round trip")
	}
	if r2.Unchanged("b.txt", b) {
		t.Error("unknown key must not be unchanged")
	}
}

func TestSentRegisterCorrupt(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	regPath := filepath.Join(dir, "sent_register.json")
	writeTestFile(t, regPath, "{ this is not json")

	r := LoadSentRegister(regPath)
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Errorf("corrupt file should have been moved away (err=%v)", err)
	}
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sent_register.json.corrupt-") {
			found = true
		}
	}
	if !found {
		t.Errorf("no .corrupt-* file in %v", entries)
	}
}

func TestSentRegisterSaveLeavesNoTemp(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	info := regFile(t, filepath.Join(dir, "a.txt"), "a", time.Unix(1600000000, 0))
	regPath := filepath.Join(dir, "sent_register.json")
	r := LoadSentRegister(regPath)
	r.Record("a.txt", info)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	entries, _ := ioutil.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "a.txt" && e.Name() != "sent_register.json" {
			t.Errorf("unexpected leftover %s", e.Name())
		}
	}
}

func TestSentRegisterSaveCleanDoesNotWrite(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	info := regFile(t, filepath.Join(dir, "a.txt"), "a", time.Unix(1600000000, 0))
	regPath := filepath.Join(dir, "sent_register.json")
	r := LoadSentRegister(regPath)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Errorf("clean register must not write a file (err=%v)", err)
	}
	r.Record("a.txt", info)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(regPath); err != nil {
		t.Fatal(err)
	}
	// Recording the same state again keeps it clean.
	r.Record("a.txt", info)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Errorf("unchanged register must not write again (err=%v)", err)
	}
}

func TestSentRegisterSaveErrorKeepsDirty(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	info := regFile(t, filepath.Join(dir, "a.txt"), "a", time.Unix(1600000000, 0))
	// The target folder does not exist, so saving fails.
	r := LoadSentRegister(filepath.Join(dir, "missing", "sent_register.json"))
	r.Record("a.txt", info)
	if err := r.Save(); err == nil {
		t.Fatal("expected an error")
	}
	if err := os.MkdirAll(filepath.Join(dir, "missing"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if LoadSentRegister(filepath.Join(dir, "missing", "sent_register.json")).Len() != 1 {
		t.Error("retry did not save the entry")
	}
}

func TestSentRegisterUnchanged(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	p := filepath.Join(dir, "a.txt")
	mt := time.Unix(1600000000, 5)
	r := newSentRegister()
	r.Record("a.txt", regFile(t, p, "abc", mt))

	if !r.Unchanged("a.txt", regFile(t, p, "abc", mt)) {
		t.Error("same mtime and size must be unchanged")
	}
	if r.Unchanged("a.txt", regFile(t, p, "abc", mt.Add(time.Second))) {
		t.Error("different mtime must be changed")
	}
	if r.Unchanged("a.txt", regFile(t, p, "abcd", mt)) {
		t.Error("different size must be changed")
	}
}

func TestSentRegisterPrune(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()
	info := regFile(t, filepath.Join(dir, "a.txt"), "a", time.Unix(1600000000, 0))
	regPath := filepath.Join(dir, "sent_register.json")
	r := LoadSentRegister(regPath)
	r.Record("keep", info)
	r.Record("gone", info)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r.Prune(map[string]bool{"keep": true, "other": true})
	if r.Len() != 1 || !r.Unchanged("keep", info) {
		t.Errorf("prune result wrong, Len=%d", r.Len())
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if LoadSentRegister(regPath).Len() != 1 {
		t.Error("prune was not saved")
	}
}
