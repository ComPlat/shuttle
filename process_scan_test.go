package main

import (
	"path/filepath"
	"testing"
	"time"
)

const scanDuration = 10 * time.Second

func scanTestManager(src, sendType string) ProcessManager {
	a := &Args{src: src, duration: scanDuration, sendType: sendType}
	return newProcessManager(a, make(chan string, 20))
}

// scanTestSetFileLocked replaces fileLockedFn for the test; call the result to restore it.
func scanTestSetFileLocked(fn func(string) bool) func() {
	old := fileLockedFn
	fileLockedFn = fn
	return func() { fileLockedFn = old }
}

func TestScanReadyFile(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	fresh := filepath.Join(src, "fresh.txt")
	ready := filepath.Join(src, "ready.txt")
	old := filepath.Join(src, "old.txt")
	for _, p := range []string{fresh, ready, old} {
		writeTestFile(t, p, "x")
	}
	setAge(t, fresh, 1*time.Second)
	setAge(t, ready, 15*time.Second)
	setAge(t, old, 60*time.Second)

	for _, sendType := range []string{"file", "flat_tar"} {
		m := scanTestManager(src, sendType)
		got, err := m.scanReady(time.Now(), make(map[string]struct{}))
		if err != nil {
			t.Fatalf("%s: %v", sendType, err)
		}
		if v, ok := got["fresh.txt"]; !ok || v {
			t.Errorf("%s: fresh.txt = %v, present=%v; want present and false", sendType, v, ok)
		}
		if v, ok := got["ready.txt"]; !ok || !v {
			t.Errorf("%s: ready.txt = %v, present=%v; want present and true", sendType, v, ok)
		}
		if _, ok := got["old.txt"]; ok {
			t.Errorf("%s: old.txt is outside the 2*duration window and must be absent", sendType)
		}
		if len(got) != 2 {
			t.Errorf("%s: got %v, want exactly 2 entries", sendType, got)
		}
	}
}

func TestScanReadyFolder(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()

	// Folder A: nested files, all 15s old -> ready.
	for _, f := range []string{
		filepath.Join("A", "B", "a.txt"),
		filepath.Join("A", "b.txt"),
		filepath.Join("A", "C", "c.txt"),
	} {
		p := filepath.Join(src, f)
		writeTestFile(t, p, "x")
		setAge(t, p, 15*time.Second)
	}
	// Folder D: one fresh file among old ones -> not ready.
	d1 := filepath.Join(src, "D", "old.txt")
	d2 := filepath.Join(src, "D", "E", "fresh.txt")
	writeTestFile(t, d1, "x")
	writeTestFile(t, d2, "x")
	setAge(t, d1, 15*time.Second)
	setAge(t, d2, 1*time.Second)
	// Folder F: only files older than the window -> not reported.
	f1 := filepath.Join(src, "F", "x.txt")
	writeTestFile(t, f1, "x")
	setAge(t, f1, 60*time.Second)
	// A loose file directly in src is its own key.
	loose := filepath.Join(src, "loose.txt")
	writeTestFile(t, loose, "x")
	setAge(t, loose, 15*time.Second)

	m := scanTestManager(src, "folder")
	got, err := m.scanReady(time.Now(), make(map[string]struct{}))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"A": true, "D": false, "loose.txt": true}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if gv, ok := got[k]; !ok || gv != v {
			t.Errorf("key %q = %v, present=%v; want %v", k, gv, ok, v)
		}
	}
}

func TestScanReadyMissingSrc(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	m := scanTestManager(filepath.Join(root, "does-not-exist"), "file")
	got, err := m.scanReady(time.Now(), make(map[string]struct{}))
	if err == nil {
		t.Error("expected an error for a missing src directory")
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty map", got)
	}
}

func TestScanReadyLockedFile(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	p := filepath.Join(src, "busy.txt")
	writeTestFile(t, p, "x")
	setAge(t, p, 60*time.Second)

	locked := true
	defer scanTestSetFileLocked(func(path string) bool { return locked && path == p })()

	m := scanTestManager(src, "file")
	lockedFiles := make(map[string]struct{})

	got, err := m.scanReady(time.Now(), lockedFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("locked file must not be reported, got %v", got)
	}
	if _, ok := lockedFiles[p]; !ok {
		t.Errorf("locked file not recorded in lockedFiles: %v", lockedFiles)
	}

	// Still locked: still nothing.
	got, _ = m.scanReady(time.Now(), lockedFiles)
	if len(got) != 0 {
		t.Errorf("still locked, got %v", got)
	}

	// Unlocked: reported ready although it is 60s old (outside the window),
	// because it was in lockedFiles. It is then removed from lockedFiles.
	locked = false
	got, err = m.scanReady(time.Now(), lockedFiles)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["busy.txt"]; !ok || !v {
		t.Errorf("busy.txt = %v, present=%v; want present and true", v, ok)
	}
	if len(lockedFiles) != 0 {
		t.Errorf("lockedFiles should be empty after unlock, got %v", lockedFiles)
	}

	// A further scan: it is old and no longer remembered, so it is absent.
	got, _ = m.scanReady(time.Now(), lockedFiles)
	if len(got) != 0 {
		t.Errorf("after the unlock scan the old file must be absent, got %v", got)
	}
}

func TestScanReadyLockedFileInFolder(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	a := filepath.Join(src, "A", "a.txt")
	b := filepath.Join(src, "A", "b.txt")
	writeTestFile(t, a, "x")
	writeTestFile(t, b, "x")
	setAge(t, a, 15*time.Second)
	setAge(t, b, 60*time.Second)
	defer scanTestSetFileLocked(func(path string) bool { return path == b })()

	m := scanTestManager(src, "folder")
	got, err := m.scanReady(time.Now(), make(map[string]struct{}))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["A"]; ok && v {
		t.Errorf("folder A contains a locked file and must not be ready, got %v", got)
	}
}

// TestDoWorkOnePass runs doWork for exactly one pass: quit is buffered, so the
// loop only sees it after the first pass and its sleep. duration is 2s, so the
// test takes about 2s; ages 3s (ready), 0s (not ready) and 60s (outside the
// window) are at least 1s away from the boundaries 2s and 4s.
func TestDoWorkOnePass(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	readyFile := filepath.Join(src, "ready.txt")
	freshFile := filepath.Join(src, "fresh.txt")
	oldFile := filepath.Join(src, "old.txt")
	for _, p := range []string{readyFile, freshFile, oldFile} {
		writeTestFile(t, p, "x")
	}
	setAge(t, readyFile, 3*time.Second)
	setAge(t, oldFile, 60*time.Second)

	a := &Args{src: src, duration: 2 * time.Second, sendType: "file"}
	doneFiles := make(chan string, 20)
	pm := newProcessManager(a, doneFiles)
	quit := make(chan int, 1)
	finished := make(chan struct{})
	go func() {
		pm.doWork(quit)
		close(finished)
	}()

	select {
	case got := <-doneFiles:
		if got != readyFile {
			t.Errorf("sent %q, want %q", got, readyFile)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for a done file")
	}
	quit <- 1
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("doWork did not stop after quit")
	}
	if n := len(doneFiles); n != 0 {
		t.Errorf("only the ready file may be sent, %d more in channel", n)
	}
}
