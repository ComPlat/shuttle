package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"time"
)

const scanDuration = 10 * time.Second

func scanTestManager(src, sendType string) ProcessManager {
	a := &Args{src: src, duration: scanDuration, sendType: sendType}
	return newProcessManager(a, make(chan string, 20), nil)
}

// scanTestSetFileLocked replaces fileLockedFn for the test; call the result to restore it.
func scanTestSetFileLocked(fn func(string) bool) func() {
	old := fileLockedFn
	fileLockedFn = fn
	return func() { fileLockedFn = old }
}

// scanTestRecord records the current state of src/rel in the register of m.
func scanTestRecord(t *testing.T, m ProcessManager, src, rel string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(src, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	m.register.Record(rel, info)
}

func readyKeys(ready map[string][]readyFile) []string {
	var keys []string
	for k := range ready {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readyRels(files []readyFile) []string {
	var rels []string
	for _, f := range files {
		rels = append(rels, f.rel)
	}
	sort.Strings(rels)
	return rels
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		got, seen, err := m.scanReady(time.Now())
		if err != nil {
			t.Fatalf("%s: %v", sendType, err)
		}
		// An old, never sent file is ready too (it used to fall out of the window).
		if keys := readyKeys(got); !sameStrings(keys, []string{"old.txt", "ready.txt"}) {
			t.Errorf("%s: ready = %v, want old.txt and ready.txt", sendType, keys)
		}
		if len(seen) != 3 {
			t.Errorf("%s: seen = %v, want 3 files", sendType, seen)
		}
		if f := got["ready.txt"]; len(f) != 1 || f[0].rel != "ready.txt" || f[0].info == nil {
			t.Errorf("%s: ready.txt entry = %v", sendType, f)
		}
	}
}

func TestScanReadyFileRegister(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	p := filepath.Join(src, "a.txt")
	writeTestFile(t, p, "x")
	setAge(t, p, 60*time.Second)
	m := scanTestManager(src, "file")
	scanTestRecord(t, m, src, "a.txt")

	got, _, err := m.scanReady(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("recorded and unchanged: got %v, want nothing", readyKeys(got))
	}

	// New mtime, same size.
	setAge(t, p, 30*time.Second)
	got, _, _ = m.scanReady(time.Now())
	if keys := readyKeys(got); !sameStrings(keys, []string{"a.txt"}) {
		t.Errorf("after mtime change: ready = %v", keys)
	}

	// Modified and still fresh: not ready.
	scanTestRecord(t, m, src, "a.txt")
	writeTestFile(t, p, "xyz")
	got, _, _ = m.scanReady(time.Now())
	if len(got) != 0 {
		t.Errorf("modified but fresh: got %v, want nothing", readyKeys(got))
	}

	// Same mtime as recorded, different size.
	setAge(t, p, 30*time.Second)
	scanTestRecord(t, m, src, "a.txt")
	info, _ := os.Stat(p)
	writeTestFile(t, p, "longer content")
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	got, _, _ = m.scanReady(time.Now())
	if keys := readyKeys(got); !sameStrings(keys, []string{"a.txt"}) {
		t.Errorf("after size change: ready = %v", keys)
	}
}

func TestScanReadyFolder(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()

	// Folder A: nested files, all stable -> ready with all files.
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
	// Folder F: very old files are ready, too.
	f1 := filepath.Join(src, "F", "x.txt")
	writeTestFile(t, f1, "x")
	setAge(t, f1, 60*time.Second)
	// A loose file directly in src is its own key.
	loose := filepath.Join(src, "loose.txt")
	writeTestFile(t, loose, "x")
	setAge(t, loose, 15*time.Second)

	for _, sendType := range []string{"folder", "zip", "tar"} {
		m := scanTestManager(src, sendType)
		got, seen, err := m.scanReady(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if keys := readyKeys(got); !sameStrings(keys, []string{"A", "F", "loose.txt"}) {
			t.Errorf("%s: ready = %v, want A, F, loose.txt", sendType, keys)
		}
		if rels := readyRels(got["A"]); !sameStrings(rels, []string{"A/B/a.txt", "A/C/c.txt", "A/b.txt"}) {
			t.Errorf("%s: files of A = %v", sendType, rels)
		}
		if len(seen) != 7 {
			t.Errorf("%s: seen = %v, want 7 files", sendType, seen)
		}
	}
}

func TestScanReadyFolderRegister(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	a := filepath.Join(src, "A", "a.txt")
	b := filepath.Join(src, "A", "sub", "b.txt")
	for _, p := range []string{a, b} {
		writeTestFile(t, p, "x")
		setAge(t, p, 60*time.Second)
	}
	m := scanTestManager(src, "folder")
	scanTestRecord(t, m, src, "A/a.txt")
	scanTestRecord(t, m, src, "A/sub/b.txt")

	got, _, err := m.scanReady(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("recorded and unchanged: got %v, want nothing", readyKeys(got))
	}

	// A new file: the whole folder is ready again, including the old files.
	c := filepath.Join(src, "A", "new.txt")
	writeTestFile(t, c, "x")
	setAge(t, c, 30*time.Second)
	got, _, _ = m.scanReady(time.Now())
	if rels := readyRels(got["A"]); !sameStrings(rels, []string{"A/a.txt", "A/new.txt", "A/sub/b.txt"}) {
		t.Errorf("files of A = %v", rels)
	}

	// The new file is still fresh: the folder is held back.
	setAge(t, c, 1*time.Second)
	got, _, _ = m.scanReady(time.Now())
	if len(got) != 0 {
		t.Errorf("fresh new file: got %v, want nothing", readyKeys(got))
	}
}

func TestScanReadyFolderOldFileFreshChange(t *testing.T) {
	// A fresh file in a folder holds it back even when all other files are
	// recorded; once stable, the whole folder is ready.
	src, cleanup := newTempDir(t)
	defer cleanup()
	a := filepath.Join(src, "A", "a.txt")
	writeTestFile(t, a, "x")
	setAge(t, a, 60*time.Second)
	m := scanTestManager(src, "zip")
	scanTestRecord(t, m, src, "A/a.txt")
	writeTestFile(t, a, "changed")
	got, _, _ := m.scanReady(time.Now())
	if len(got) != 0 {
		t.Errorf("fresh change: got %v", readyKeys(got))
	}
	setAge(t, a, 60*time.Second)
	got, _, _ = m.scanReady(time.Now())
	if keys := readyKeys(got); !sameStrings(keys, []string{"A"}) {
		t.Errorf("stable change: ready = %v", keys)
	}
}

func TestScanReadyMissingSrc(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	m := scanTestManager(filepath.Join(root, "does-not-exist"), "file")
	got, seen, err := m.scanReady(time.Now())
	if err == nil {
		t.Error("expected an error for a missing src directory")
	}
	if len(got) != 0 || len(seen) != 0 {
		t.Errorf("got %v / %v, want empty maps", got, seen)
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
	got, seen, err := m.scanReady(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("locked file must not be ready, got %v", readyKeys(got))
	}
	if !seen["busy.txt"] {
		t.Errorf("locked file must still be in seen: %v", seen)
	}

	// Unlocked: ready on the next scan, without any memory of the lock.
	locked = false
	got, _, err = m.scanReady(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if keys := readyKeys(got); !sameStrings(keys, []string{"busy.txt"}) {
		t.Errorf("after unlock: ready = %v", keys)
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
	locked := true
	defer scanTestSetFileLocked(func(path string) bool { return locked && path == b })()

	m := scanTestManager(src, "folder")
	got, _, err := m.scanReady(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("folder A contains a locked file and must not be ready, got %v", readyKeys(got))
	}
	locked = false
	got, _, _ = m.scanReady(time.Now())
	if keys := readyKeys(got); !sameStrings(keys, []string{"A"}) {
		t.Errorf("after unlock: ready = %v", keys)
	}
}

// onePassDuration is the -duration the doWork tests use. A pass over a few
// files takes milliseconds, so onePass waits half of it before asking doWork to
// stop; the loop checks quit only after the sleep that ends the first pass.
const onePassDuration = 2 * time.Second

// onePass runs doWork for exactly one pass (about onePassDuration long).
func onePass(t *testing.T, pm ProcessManager) {
	t.Helper()
	quit := make(chan int, 1)
	finished := make(chan struct{})
	go func() {
		pm.doWork(quit)
		close(finished)
	}()
	time.Sleep(onePassDuration / 2)
	quit <- 1
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("doWork did not stop after quit")
	}
}

func drain(ch chan string) []string {
	var out []string
	for {
		select {
		case s := <-ch:
			out = append(out, s)
		default:
			sort.Strings(out)
			return out
		}
	}
}

func TestDoWorkSendsRecordsAndRestarts(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	reg, cleanupReg := newTempDir(t)
	defer cleanupReg()
	regPath := filepath.Join(reg, "sent_register.json")

	a := &Args{src: src, duration: onePassDuration, sendType: "file"}
	p := filepath.Join(src, "ready.txt")
	writeTestFile(t, p, "x")
	setAge(t, p, 60*time.Second)

	// Pass 1: the file is sent and recorded.
	done1 := make(chan string, 20)
	onePass(t, newProcessManager(a, done1, LoadSentRegister(regPath)))
	if got := drain(done1); !sameStrings(got, []string{p}) {
		t.Fatalf("pass 1 sent %v, want %v", got, p)
	}

	// Restart: a new manager loads the register from disk and sends nothing.
	done2 := make(chan string, 20)
	r2 := LoadSentRegister(regPath)
	if r2.Len() != 1 {
		t.Fatalf("register has %d entries after restart, want 1", r2.Len())
	}
	onePass(t, newProcessManager(a, done2, r2))
	if got := drain(done2); len(got) != 0 {
		t.Errorf("after restart sent %v, want nothing", got)
	}

	// Modified file: sent again.
	writeTestFile(t, p, "modified content")
	setAge(t, p, 60*time.Second)
	done3 := make(chan string, 20)
	onePass(t, newProcessManager(a, done3, LoadSentRegister(regPath)))
	if got := drain(done3); !sameStrings(got, []string{p}) {
		t.Errorf("after modification sent %v, want %v", got, p)
	}
}

func TestDoWorkOnePass(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	readyFile := filepath.Join(src, "ready.txt")
	oldFile := filepath.Join(src, "old.txt")
	freshFile := filepath.Join(src, "fresh.txt")
	for _, p := range []string{readyFile, oldFile, freshFile} {
		writeTestFile(t, p, "x")
	}
	setAge(t, readyFile, 30*time.Second)
	setAge(t, oldFile, 600*time.Second)
	// fresh.txt was just written and is not stable yet.

	a := &Args{src: src, duration: onePassDuration, sendType: "file"}
	doneFiles := make(chan string, 20)
	onePass(t, newProcessManager(a, doneFiles, nil))
	if got := drain(doneFiles); !sameStrings(got, []string{oldFile, readyFile}) {
		t.Errorf("sent %v, want old.txt and ready.txt", got)
	}
}

func TestDoWorkPruneKeepsRegisterWhenSrcMissing(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	src := filepath.Join(root, "src")
	p := filepath.Join(src, "a.txt")
	writeTestFile(t, p, "x")
	setAge(t, p, 60*time.Second)
	regPath := filepath.Join(root, "sent_register.json")
	a := &Args{src: src, duration: onePassDuration, sendType: "file"}
	onePass(t, newProcessManager(a, make(chan string, 20), LoadSentRegister(regPath)))

	// Drive unreachable: the register must survive.
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	onePass(t, newProcessManager(a, make(chan string, 20), LoadSentRegister(regPath)))
	if n := LoadSentRegister(regPath).Len(); n != 1 {
		t.Errorf("register has %d entries after a failed scan, want 1", n)
	}

	// Root reachable again but the file is deleted: pruned.
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	onePass(t, newProcessManager(a, make(chan string, 20), LoadSentRegister(regPath)))
	if n := LoadSentRegister(regPath).Len(); n != 0 {
		t.Errorf("register has %d entries after deleting the file, want 0", n)
	}
}

func TestDoWorkFlatTarRecordsAfterCopy(t *testing.T) {
	_, restore := useTempGlobals(t)
	defer restore()
	src, cleanup := newTempDir(t)
	defer cleanup()
	good := filepath.Join(src, "a_1.txt")
	writeTestFile(t, good, "A1")
	setAge(t, good, 60*time.Second)

	a := &Args{src: src, duration: onePassDuration, sendType: "flat_tar", commonRegex: regexp.MustCompile(`^(\w)_`)}
	doneFiles := make(chan string, 20)
	reg := newSentRegister()
	onePass(t, newProcessManager(a, doneFiles, reg))
	// Prefixes are collected before copying, so the tar follows in the next pass.
	if got := drain(doneFiles); len(got) != 0 {
		t.Errorf("pass 1 sent %v, want nothing yet", got)
	}
	if _, err := os.Stat(filepath.Join(FlatTarTempPath, "a_1.txt")); err != nil {
		t.Errorf("a_1.txt was not copied: %v", err)
	}
	if reg.Len() != 1 {
		t.Errorf("successful copy must be recorded, Len = %d", reg.Len())
	}

	// The copy fails (the file vanishes after the scan): nothing is recorded.
	bad := filepath.Join(src, "b_1.txt")
	writeTestFile(t, bad, "B1")
	setAge(t, bad, 60*time.Second)
	defer scanTestSetFileLocked(func(path string) bool {
		if path == bad {
			_ = os.Remove(bad)
		}
		return false
	})()
	onePass(t, newProcessManager(a, doneFiles, reg))
	// Only the tar of the first file is sent; b_1.txt never reached the temp folder.
	if got := drain(doneFiles); len(got) != 1 || filepath.Base(got[0]) != "a.tar.gz" {
		t.Errorf("pass 2 sent %v, want only a.tar.gz", got)
	}
	if _, err := os.Stat(filepath.Join(FlatTarTempPath, "b_1.txt")); !os.IsNotExist(err) {
		t.Errorf("b_1.txt must not be in the temp folder (err=%v)", err)
	}
	info := mustStat(t, good)
	if reg.Len() != 1 || !reg.Unchanged("a_1.txt", info) {
		t.Errorf("failed copy must not be recorded, Len = %d", reg.Len())
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// Unchanged files must never be lock-checked (the check renames the file onto
// itself); only files of groups that are about to be sent are.
func TestScanReadyLockCheckOnlyForCandidates(t *testing.T) {
	src, cleanup := newTempDir(t)
	defer cleanup()
	for _, f := range []string{
		filepath.Join("A", "a1.txt"), filepath.Join("A", "a2.txt"),
		filepath.Join("B", "b1.txt"), filepath.Join("B", "b2.txt"),
	} {
		p := filepath.Join(src, f)
		writeTestFile(t, p, "x")
		setAge(t, p, 60*time.Second)
	}
	reg := newSentRegister()
	for _, rel := range []string{"A/a1.txt", "A/a2.txt"} {
		info, err := os.Stat(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		reg.Record(rel, info)
	}

	checked := map[string]int{}
	defer scanTestSetFileLocked(func(path string) bool {
		rel, _ := filepath.Rel(src, path)
		checked[filepath.ToSlash(rel)]++
		return false
	})()

	for _, sendType := range []string{"folder", "file"} {
		for k := range checked {
			delete(checked, k)
		}
		a := &Args{src: src, duration: scanDuration, sendType: sendType}
		m := newProcessManager(a, make(chan string, 20), reg)
		ready, _, err := m.scanReady(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if checked["A/a1.txt"] != 0 || checked["A/a2.txt"] != 0 {
			t.Errorf("%s: unchanged files were lock-checked: %v", sendType, checked)
		}
		if checked["B/b1.txt"] != 1 || checked["B/b2.txt"] != 1 {
			t.Errorf("%s: candidate files must be lock-checked once: %v", sendType, checked)
		}
		if len(ready) == 0 {
			t.Errorf("%s: B must be ready, got %v", sendType, ready)
		}
	}
}
