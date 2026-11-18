package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type sentCall struct {
	local, rel, content string
}

// fakeTransfer records every send_file call. fail decides per remoteRel.
type fakeTransfer struct {
	calls []sentCall
	fail  map[string]bool
}

func (f *fakeTransfer) doWork(quit chan int)     {}
func (f *fakeTransfer) connect_to_server() error { return nil }
func (f *fakeTransfer) send_file(localPath, remoteRel string) (bool, error) {
	data, _ := ioutil.ReadFile(localPath)
	f.calls = append(f.calls, sentCall{localPath, remoteRel, string(data)})
	if f.fail[remoteRel] {
		return false, errors.New("scripted failure")
	}
	return true, nil
}

func (f *fakeTransfer) rels() []string {
	var r []string
	for _, c := range f.calls {
		r = append(r, c.rel)
	}
	sort.Strings(r)
	return r
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	items, err := ReadDirCompat(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, i := range items {
		n = append(n, i.Name())
	}
	return n
}

func TestTransferCycleFile(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	writeTestFile(t, filepath.Join(TempPath, "ok.txt"), "ok")
	writeTestFile(t, filepath.Join(TempPath, "bad.txt"), "bad")
	f := &fakeTransfer{fail: map[string]bool{"bad.txt": true}}

	if n := transferCycle(f, TempPath, "file", 0); n != 1 {
		t.Fatalf("failures = %d, want 1", n)
	}
	if exists(filepath.Join(TempPath, "ok.txt")) {
		t.Error("sent file was not removed")
	}
	if !exists(filepath.Join(TempPath, "bad.txt")) {
		t.Error("failed file was removed")
	}
}

func makeFolder(t *testing.T) string {
	folder := filepath.Join(TempPath, "A")
	writeTestFile(t, filepath.Join(folder, "x.txt"), "xx")
	writeTestFile(t, filepath.Join(folder, "B", "y.txt"), "yy")
	return folder
}

func TestTransferCycleZipAndTar(t *testing.T) {
	for _, mode := range []string{"zip", "tar"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			_, cleanup := useTempGlobals(t)
			defer cleanup()
			folder := makeFolder(t)
			f := &fakeTransfer{}
			if n := transferCycle(f, TempPath, mode, time.Millisecond); n != 0 {
				t.Fatalf("failures = %d", n)
			}
			want := "A.zip"
			if mode == "tar" {
				want = "A.tar.gz"
			}
			if len(f.calls) != 1 || f.calls[0].rel != want {
				t.Fatalf("calls = %v, want one %s", f.rels(), want)
			}
			// The content of the archive was captured at call time.
			tmp := filepath.Join(ArchivePath, "check")
			if err := ioutil.WriteFile(tmp, []byte(f.calls[0].content), 0644); err != nil {
				t.Fatal(err)
			}
			var entries map[string]string
			if mode == "zip" {
				entries = readZipEntries(t, tmp)
			} else {
				entries = readTarGzEntries(t, tmp)
			}
			if err := os.Remove(tmp); err != nil {
				t.Fatal(err)
			}
			got := 0
			for name, c := range entries {
				if strings.HasSuffix(name, "x.txt") && c == "xx" || strings.HasSuffix(name, "y.txt") && c == "yy" {
					got++
				}
			}
			if got != 2 {
				t.Errorf("archive entries = %v", entries)
			}
			if exists(folder) {
				t.Error("folder not removed")
			}
			if n := dirNames(t, TempPath); len(n) != 0 {
				t.Errorf("srcDir not empty: %v", n)
			}
			if n := dirNames(t, ArchivePath); len(n) != 0 {
				t.Errorf("ArchivePath not empty: %v", n)
			}
		})
	}
}

func TestTransferCycleZipFailureRetriesOnce(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	folder := makeFolder(t)
	f := &fakeTransfer{fail: map[string]bool{"A.zip": true}}

	if n := transferCycle(f, TempPath, "zip", 0); n != 1 {
		t.Fatalf("failures = %d, want 1", n)
	}
	if !exists(folder) {
		t.Error("folder removed after failed send")
	}
	if n := dirNames(t, TempPath); len(n) != 1 || n[0] != "A" {
		t.Errorf("srcDir = %v, want only A", n)
	}
	if n := dirNames(t, ArchivePath); len(n) != 0 {
		t.Errorf("ArchivePath not empty: %v", n)
	}

	f.fail = nil
	if n := transferCycle(f, TempPath, "zip", 0); n != 0 {
		t.Fatalf("second cycle failures = %d", n)
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %v, want one per cycle", f.rels())
	}
	if exists(folder) || len(dirNames(t, ArchivePath)) != 0 {
		t.Error("leftovers after successful retry")
	}
}

func TestTransferCycleFolderMode(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	folder := makeFolder(t)
	f := &fakeTransfer{fail: map[string]bool{"A/B/y.txt": true}}

	done := make(chan int, 1)
	go func() { done <- transferCycle(f, TempPath, "folder", 0) }()
	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("failures = %d, want 1", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cycle does not terminate")
	}
	rels := f.rels()
	if len(rels) != 2 || rels[0] != "A/B/y.txt" || rels[1] != "A/x.txt" {
		t.Fatalf("rels = %v", rels)
	}
	if exists(filepath.Join(folder, "x.txt")) {
		t.Error("sent file kept")
	}
	if !exists(filepath.Join(folder, "B", "y.txt")) {
		t.Error("failed file removed")
	}

	f.fail = nil
	if n := transferCycle(f, TempPath, "folder", 0); n != 0 {
		t.Fatalf("second cycle failures = %d", n)
	}
	if exists(folder) {
		t.Error("empty folder not removed")
	}
}

func TestRemoteRelPath(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	cases := []struct {
		src, local, want string
		err              bool
	}{
		{TempPath, filepath.Join(TempPath, "a.txt"), "a.txt", false},
		{TempPath, filepath.Join(TempPath, "A", "B", "x.txt"), "A/B/x.txt", false},
		{TempPath, filepath.Join(TempConvertedPath, "x.zip"), "", true},
		{TempPath, filepath.Dir(TempPath), "", true},
		{TempConvertedPath, filepath.Join(TempConvertedPath, "x.zip"), "x.zip", false},
	}
	for _, c := range cases {
		got, err := remoteRelPath(c.src, c.local)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("remoteRelPath(%q, %q) = %q, %v", c.src, c.local, got, err)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	max := 30 * time.Second
	cases := []struct {
		n    int
		want time.Duration
	}{
		{-1, 0}, {0, 0}, {1, time.Second}, {2, 2 * time.Second}, {3, 4 * time.Second},
		{5, 16 * time.Second}, {6, max}, {7, max}, {1000, max}, {1 << 30, max},
	}
	for _, c := range cases {
		if got := backoffDelay(c.n, max); got != c.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", c.n, got, c.want)
		}
	}
	if got := backoffDelay(1, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Errorf("cap below 1s = %v", got)
	}
}

func TestTransferCycleManyFilesNoPause(t *testing.T) {
	_, cleanup := useTempGlobals(t)
	defer cleanup()
	for i := 0; i < 100; i++ {
		writeTestFile(t, filepath.Join(TempPath, fmt.Sprintf("f%03d.txt", i)), "x")
	}
	f := &fakeTransfer{}
	start := time.Now()
	if n := transferCycle(f, TempPath, "file", 0); n != 0 {
		t.Fatalf("failures = %d", n)
	}
	if len(f.calls) != 100 {
		t.Fatalf("calls = %d, want 100", len(f.calls))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("100 files took %v", d)
	}
}
