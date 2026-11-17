package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestFlatTarPrefixes(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	writeTestFile(t, filepath.Join(FlatTarTempPath, "a_1.txt"), "A1")
	writeTestFile(t, filepath.Join(FlatTarTempPath, "a_2.txt"), "A2")
	writeTestFile(t, filepath.Join(FlatTarTempPath, "b_1.txt"), "B1")

	doneFiles := make(chan string, 10)
	a := &Args{commonRegex: regexp.MustCompile(`^(\w)_`), sendType: "flat_tar"}
	pm := newProcessManager(a, doneFiles)

	pm.collectTarPrefixes()
	if len(pm.doneFlatPrefixes) != 2 {
		t.Fatalf("doneFlatPrefixes = %v, want 2 prefixes a and b", pm.doneFlatPrefixes)
	}
	for _, k := range []string{"a", "b"} {
		if _, ok := pm.doneFlatPrefixes[k]; !ok {
			t.Errorf("missing prefix %q in %v", k, pm.doneFlatPrefixes)
		}
	}

	pm.processTarPrefixes()
	if len(pm.doneFlatPrefixes) != 0 {
		t.Errorf("doneFlatPrefixes should be empty afterwards, got %v", pm.doneFlatPrefixes)
	}
	close(doneFiles)
	var tars []string
	for p := range doneFiles {
		tars = append(tars, p)
	}
	if len(tars) != 2 {
		t.Fatalf("got %d tar files %v, want 2", len(tars), tars)
	}

	want := map[string]map[string]string{
		"a.tar.gz": {"a_1.txt": "A1", "a_2.txt": "A2"},
		"b.tar.gz": {"b_1.txt": "B1"},
	}
	for _, p := range tars {
		base := filepath.Base(p)
		// WP 3.7: finished archives are handed over from FlatTarOutPath.
		if filepath.Dir(p) != FlatTarOutPath {
			t.Errorf("tar %s is not inside FlatTarOutPath %s", p, FlatTarOutPath)
		}
		w, ok := want[base]
		if !ok {
			t.Errorf("unexpected tar %s", p)
			continue
		}
		got := readTarGzEntries(t, p)
		if len(got) != len(w) {
			t.Errorf("%s: entries %v, want %v", base, got, w)
		}
		for name, content := range w {
			if got[name] != content {
				t.Errorf("%s: entry %s = %q, want %q", base, name, got[name], content)
			}
		}
		delete(want, base)
	}

	// The loose source files and the temporary group folders are gone.
	for _, n := range []string{"a_1.txt", "a_2.txt", "b_1.txt", "a", "b"} {
		if _, err := os.Stat(filepath.Join(FlatTarTempPath, n)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed (err=%v)", n, err)
		}
	}
}

func TestProcessTarPrefixesSpecialGroupNames(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	// The group "x:y/z" is not a valid folder name on Windows.
	writeTestFile(t, filepath.Join(FlatTarTempPath, "x:y_z__1.txt"), "one")
	writeTestFile(t, filepath.Join(FlatTarTempPath, "keep.txt"), "later")

	doneFiles := make(chan string, 10)
	a := &Args{commonRegex: regexp.MustCompile(`^x(:y)_(z)__`), sendType: "flat_tar"}
	pm := newProcessManager(a, doneFiles)
	// Simulate a group name containing separators and a colon.
	pm.doneFlatPrefixes = map[string][]string{"x:y/z": {":y", "z"}}

	pm.processTarPrefixes()
	close(doneFiles)
	var tars []string
	for p := range doneFiles {
		tars = append(tars, p)
	}
	if len(tars) != 1 {
		t.Fatalf("tars = %v, want exactly one", tars)
	}
	if filepath.Dir(tars[0]) != FlatTarOutPath {
		t.Errorf("tar %s not inside FlatTarOutPath", tars[0])
	}
	if filepath.Base(tars[0]) != "x_y_z.tar.gz" {
		t.Errorf("tar name = %s, want x_y_z.tar.gz", filepath.Base(tars[0]))
	}
	got := readTarGzEntries(t, tars[0])
	if len(got) != 1 || got["x:y_z__1.txt"] != "one" {
		t.Errorf("tar entries = %v", got)
	}
	// Only the file of the group not processed is left in FlatTarTempPath.
	left, err := ReadDirCompat(FlatTarTempPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != "keep.txt" {
		t.Errorf("FlatTarTempPath should only hold keep.txt, got %v", left)
	}
}

func TestProcessTarPrefixesTarFailureKeepsSources(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	writeTestFile(t, filepath.Join(FlatTarTempPath, "a_1.txt"), "A1")
	// A directory in the way of the archive name makes tarFolder fail at Create.
	if err := os.MkdirAll(filepath.Join(FlatTarTempPath, "a.tar.gz"), 0755); err != nil {
		t.Fatal(err)
	}

	doneFiles := make(chan string, 10)
	a := &Args{commonRegex: regexp.MustCompile(`^(\w)_`), sendType: "flat_tar"}
	pm := newProcessManager(a, doneFiles)
	pm.collectTarPrefixes()
	pm.processTarPrefixes()

	if len(doneFiles) != 0 {
		t.Errorf("nothing may be sent when tarring fails, got %d", len(doneFiles))
	}
	b, err := ioutil.ReadFile(filepath.Join(FlatTarTempPath, "a_1.txt"))
	if err != nil || string(b) != "A1" {
		t.Errorf("source must be moved back, got %q err=%v", b, err)
	}
	if _, err := os.Stat(filepath.Join(FlatTarTempPath, "a")); !os.IsNotExist(err) {
		t.Errorf("group folder should be removed (err=%v)", err)
	}
	left, _ := ReadDirCompat(FlatTarOutPath)
	if len(left) != 0 {
		t.Errorf("FlatTarOutPath should be empty, got %v", left)
	}
}

func TestProcessTarPrefixesKeepsPendingTar(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	writeTestFile(t, filepath.Join(FlatTarOutPath, "a.tar.gz"), "pending")
	writeTestFile(t, filepath.Join(FlatTarTempPath, "a_1.txt"), "A1")

	doneFiles := make(chan string, 10)
	a := &Args{commonRegex: regexp.MustCompile(`^(\w)_`), sendType: "flat_tar"}
	pm := newProcessManager(a, doneFiles)
	pm.collectTarPrefixes()
	pm.processTarPrefixes()

	got := <-doneFiles
	if filepath.Base(got) != "a_dup1.tar.gz" {
		t.Errorf("sent %s, want a_dup1.tar.gz", got)
	}
	b, _ := ioutil.ReadFile(filepath.Join(FlatTarOutPath, "a.tar.gz"))
	if string(b) != "pending" {
		t.Errorf("pending tar was overwritten: %q", b)
	}
}
