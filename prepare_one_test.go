package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareOneFlatTarCopyFailureKeepsSource(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()

	// PreTempPath is a file, so the copy cannot succeed.
	if err := os.RemoveAll(PreTempPath); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, PreTempPath, "not a directory")

	src := filepath.Join(root, "out", "a.tar.gz")
	writeTestFile(t, src, "tar content")

	m := newPrepareManager(&Args{sendType: "flat_tar"}, make(chan string))
	if err := m.prepareOne(src); err == nil {
		t.Error("prepareOne must return the copy error")
	}
	b, err := ioutil.ReadFile(src)
	if err != nil || string(b) != "tar content" {
		t.Errorf("source must be kept, got %q err=%v", b, err)
	}
}

func TestPrepareOneMissingSource(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	m := newPrepareManager(&Args{sendType: "flat_tar"}, make(chan string))
	if err := m.prepareOne(filepath.Join(root, "gone.tar.gz")); err == nil {
		t.Error("prepareOne of a missing source must fail")
	}
}

func TestPrepareOneFlatTarSuccess(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()

	src := filepath.Join(root, "out", "a.tar.gz")
	writeTestFile(t, src, "tar content")

	m := newPrepareManager(&Args{sendType: "flat_tar"}, make(chan string))
	if err := m.prepareOne(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source should be removed after a successful copy (err=%v)", err)
	}
	b, err := ioutil.ReadFile(filepath.Join(TempPath, "a.tar.gz"))
	if err != nil || string(b) != "tar content" {
		t.Errorf("TempPath content = %q err=%v", b, err)
	}
}

func TestPrepareOneFolderKeepsSource(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()

	src := filepath.Join(root, "in", "A")
	writeTestFile(t, filepath.Join(src, "sub", "x.txt"), "x")

	m := newPrepareManager(&Args{sendType: "folder"}, make(chan string))
	if err := m.prepareOne(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("folder source must stay for non flat_tar types: %v", err)
	}
	b, err := ioutil.ReadFile(filepath.Join(TempPath, "A", "sub", "x.txt"))
	if err != nil || string(b) != "x" {
		t.Errorf("TempPath content = %q err=%v", b, err)
	}
}

func TestSafeDirName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", "abc"},
		{"a:b", "a_b"},
		{"a/b\\c", "a_b_c"},
		{"<>:\"/\\|?*", "_________"},
		{"a\tb\nc", "a_b_c"},
		{"name. . ", "name"},
		{"", "_archive"},
		{"...", "_archive"},
		{" ", "_archive"},
		{"x___y", "x___y"},
	}
	for _, c := range cases {
		if got := safeDirName(c.in); got != c.want {
			t.Errorf("safeDirName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUniqueName(t *testing.T) {
	dir, cleanup := newTempDir(t)
	defer cleanup()

	if got := uniqueName(dir, "a.tar.gz"); got != "a.tar.gz" {
		t.Errorf("free name changed to %q", got)
	}
	writeTestFile(t, filepath.Join(dir, "a.tar.gz"), "1")
	if got := uniqueName(dir, "a.tar.gz"); got != "a_dup1.tar.gz" {
		t.Errorf("got %q, want a_dup1.tar.gz", got)
	}
	writeTestFile(t, filepath.Join(dir, "a_dup1.tar.gz"), "2")
	writeTestFile(t, filepath.Join(dir, "a_dup3.tar.gz"), "3")
	if got := uniqueName(dir, "a.tar.gz"); got != "a_dup2.tar.gz" {
		t.Errorf("got %q, want the smallest free a_dup2.tar.gz", got)
	}
	writeTestFile(t, filepath.Join(dir, "noext"), "4")
	if got := uniqueName(dir, "noext"); got != "noext_dup1" {
		t.Errorf("got %q, want noext_dup1", got)
	}
}
