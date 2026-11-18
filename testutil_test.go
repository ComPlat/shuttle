package main

// Shared test helpers. Test code is vetted by the Go 1.10.8 XP build
// (dev/build_xp.sh runs "go vet ."), so it follows the same Go 1.10 rules as
// production code: no t.TempDir, t.Cleanup, t.Setenv, os.ReadFile/WriteFile,
// os.MkdirTemp or io.Discard. Use the helpers below instead.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Production code logs through these globals; they are nil until
	// initTool runs, so every test would panic without this.
	InfoLogger = log.New(ioutil.Discard, "-> INFO: ", 0)
	ErrorLogger = log.New(os.Stderr, "ERROR: ", log.Lshortfile)
	os.Exit(m.Run())
}

// newTempDir creates a fresh temporary directory. Call the returned function
// (usually with defer) to remove it.
func newTempDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "shuttle-test-")
	if err != nil {
		t.Fatal(err)
	}
	return dir, func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("cleanup %s: %v", dir, err)
		}
	}
}

// writeTestFile creates path (and its parent directories) with content.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// setAge sets the modification time of path to now minus age.
func setAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

// useTempGlobals points every working-directory global (TempPath,
// PreTempPath, FlatTarTempPath, TempConvertedPath, PreScriptPath,
// IncomingPath, ArchivePath, FlatTarOutPath, LogPath)
// at fresh subdirectories of a temp dir, so no test touches ~/shuttle.
// The returned function restores the old values and removes the temp dir.
func useTempGlobals(t *testing.T) (string, func()) {
	t.Helper()
	root, cleanup := newTempDir(t)
	old := []string{TempPath, PreTempPath, FlatTarTempPath, TempConvertedPath, PreScriptPath,
		IncomingPath, ArchivePath, FlatTarOutPath, LogPath, KnownHostsPath}

	TempPath = filepath.Join(root, "shuttle_temp")
	PreTempPath = filepath.Join(root, "shuttle_pre_temp")
	FlatTarTempPath = filepath.Join(root, "shuttle_flat_tar_temp")
	TempConvertedPath = filepath.Join(root, "shuttle_converted_temp")
	PreScriptPath = filepath.Join(root, "scripts")
	IncomingPath = filepath.Join(root, "shuttle_incoming")
	ArchivePath = filepath.Join(root, "shuttle_archive_temp")
	FlatTarOutPath = filepath.Join(root, "shuttle_flat_tar_out")
	LogPath = filepath.Join(root, "shuttle_log.txt")
	KnownHostsPath = filepath.Join(root, "known_hosts")
	for _, p := range []string{TempPath, PreTempPath, FlatTarTempPath, TempConvertedPath, PreScriptPath,
		IncomingPath, ArchivePath, FlatTarOutPath} {
		if err := os.MkdirAll(p, 0755); err != nil {
			cleanup()
			t.Fatal(err)
		}
	}

	return root, func() {
		TempPath, PreTempPath, FlatTarTempPath, TempConvertedPath, PreScriptPath = old[0], old[1], old[2], old[3], old[4]
		IncomingPath, ArchivePath, FlatTarOutPath, LogPath = old[5], old[6], old[7], old[8]
		KnownHostsPath = old[9]
		cleanup()
	}
}

// readZipEntries returns entry name -> content, with names exactly as stored.
func readZipEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := ioutil.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = string(b)
	}
	return got
}

// readTarGzEntries returns entry name -> content, with names exactly as stored.
func readTarGzEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := ioutil.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = string(b)
	}
	// Reading the gzip stream to its end verifies the trailer (closed writer).
	if _, err := io.Copy(ioutil.Discard, gr); err != nil {
		t.Fatal(err)
	}
	if err := gr.Close(); err != nil {
		t.Fatal(err)
	}
	return got
}
