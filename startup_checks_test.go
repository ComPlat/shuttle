package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withBaseDir points exeDir at dir (APP_BASE_DIR) and returns the restore func.
func withBaseDir(dir string) func() {
	old, had := os.LookupEnv("APP_BASE_DIR")
	os.Setenv("APP_BASE_DIR", dir)
	return func() {
		if had {
			os.Setenv("APP_BASE_DIR", old)
		} else {
			os.Unsetenv("APP_BASE_DIR")
		}
	}
}

func converterFileName() string {
	if runtime.GOOS == "windows" {
		return "ChemConverterApp.exe"
	}
	return "ChemConverterApp"
}

func TestStartupChecksMissingScriptsFolder(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	PreScriptPath = filepath.Join(root, "no_such_scripts")
	if err := startupChecks(&Args{}); err == nil {
		t.Error("a missing scripts folder must be an error")
	}
}

func TestStartupChecksPreconvertMissingConverter(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	defer withBaseDir(filepath.Join(root, "empty_base"))()
	if err := startupChecks(&Args{preconvert: true}); err == nil {
		t.Error("preconvert without a converter must be an error")
	}
	// Without -preconvert the converter is not needed.
	if err := startupChecks(&Args{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStartupChecksConverterIsDirectory(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	base := filepath.Join(root, "base")
	if err := os.MkdirAll(filepath.Join(base, converterFileName()), 0755); err != nil {
		t.Fatal(err)
	}
	defer withBaseDir(base)()
	if err := startupChecks(&Args{preconvert: true}); err == nil {
		t.Error("a directory is not a converter")
	}
}

func TestStartupChecksOK(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	base := filepath.Join(root, "base")
	writeTestFile(t, filepath.Join(base, converterFileName()), "x")
	defer withBaseDir(base)()
	if err := startupChecks(&Args{preconvert: true}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPreScriptsMissingFolderDoesNotExit(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	PreScriptPath = filepath.Join(root, "no_such_scripts")
	RunPreScripts(filepath.Join(root, "whatever")) // must return, not exit
}

func TestRunConverterScriptsMissingReturnsError(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	defer withBaseDir(filepath.Join(root, "empty_base"))()
	if err := RunConverterScripts(filepath.Join(root, "x")); err == nil {
		t.Error("expected an error for a missing converter")
	}
}

func TestPrepareOnePreconvertMissingConverterStillHandsOver(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	defer withBaseDir(filepath.Join(root, "empty_base"))()

	src := filepath.Join(root, "in", "sample")
	writeTestFile(t, filepath.Join(src, "a.txt"), "raw data")

	m := newPrepareManager(&Args{sendType: "zip", preconvert: true}, make(chan string))
	if err := m.prepareOne(src); err != nil {
		t.Fatalf("prepareOne: %v", err)
	}
	b, err := ioutil.ReadFile(filepath.Join(TempPath, "sample", "a.txt"))
	if err != nil || string(b) != "raw data" {
		t.Errorf("raw entry must reach TempPath, got %q err=%v", b, err)
	}
}

func TestPrepareOneHandOverFailureKeepsEntries(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()

	// IncomingPath is a file: the hand-over cannot work.
	if err := os.RemoveAll(IncomingPath); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, IncomingPath, "blocker")

	src := filepath.Join(root, "in", "sample")
	writeTestFile(t, filepath.Join(src, "a.txt"), "raw data")

	m := newPrepareManager(&Args{sendType: "zip"}, make(chan string))
	if err := m.prepareOne(src); err == nil {
		t.Error("prepareOne must return the hand-over error")
	}
	if _, err := os.Stat(filepath.Join(PreTempPath, "sample", "a.txt")); err != nil {
		t.Errorf("entry must stay in PreTempPath: %v", err)
	}
}

func TestFlatTarReadDirErrorDoesNotExit(t *testing.T) {
	root, restore := useTempGlobals(t)
	defer restore()
	FlatTarTempPath = filepath.Join(root, "no_such_flat_tar")
	m := newProcessManager(&Args{}, make(chan string, 1), nil)
	m.collectTarPrefixes()
	m.processTarPrefixes()
}
