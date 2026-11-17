package main

import (
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
