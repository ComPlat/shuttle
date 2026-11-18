package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A hand-over blocked by an entry the transfer manager is still working on
// must be retried even when no new item arrives (regression for the
// interaction of WP 3.3 folder mode and WP 3.4 atomic hand-over).
func TestRetryPendingHandOver(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	writeTestFile(t, filepath.Join(TempPath, "A", "old.txt"), "old")
	writeTestFile(t, filepath.Join(IncomingPath, "A", "new.txt"), "new")

	m := newPrepareManager(&Args{duration: time.Second}, make(chan string))
	m.retryPendingHandOver()
	if _, err := os.Stat(filepath.Join(IncomingPath, "A")); err != nil {
		t.Fatalf("hand-over must stay pending while TempPath/A exists: %v", err)
	}

	// The transfer manager finishes and removes the old folder.
	if err := os.RemoveAll(filepath.Join(TempPath, "A")); err != nil {
		t.Fatal(err)
	}
	m.retryPendingHandOver()
	if _, err := os.Stat(filepath.Join(TempPath, "A", "new.txt")); err != nil {
		t.Errorf("pending entry was not handed over: %v", err)
	}
	if _, err := os.Stat(filepath.Join(IncomingPath, "A")); !os.IsNotExist(err) {
		t.Errorf("IncomingPath/A must be gone, stat err = %v", err)
	}
}

func TestPrepareDoWorkRetriesWithoutNewItems(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	writeTestFile(t, filepath.Join(IncomingPath, "B", "x.txt"), "x")

	m := newPrepareManager(&Args{duration: time.Second}, make(chan string))
	quit := make(chan int)
	done := make(chan struct{})
	go func() {
		m.doWork(quit)
		close(done)
	}()
	defer func() {
		close(quit)
		<-done
	}()

	target := filepath.Join(TempPath, "B", "x.txt")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(target); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("doWork did not retry the pending hand-over within 10s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// After a failed copy the entries are still in PreTempPath (not IncomingPath);
// the periodic retry must hand them over too.
func TestRetryPendingHandOverFromPreTemp(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()

	writeTestFile(t, filepath.Join(PreTempPath, "C", "c.txt"), "c")

	m := newPrepareManager(&Args{duration: time.Second}, make(chan string))
	m.retryPendingHandOver()
	if _, err := os.Stat(filepath.Join(TempPath, "C", "c.txt")); err != nil {
		t.Errorf("entry left in PreTempPath was not handed over: %v", err)
	}
}
