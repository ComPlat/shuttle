package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// pathGlobals returns pointers to every global setupPaths sets.
func pathGlobals() []*string {
	return []*string{&TempPath, &TempConvertedPath, &PreTempPath, &FlatTarTempPath,
		&IncomingPath, &ArchivePath, &FlatTarOutPath, &RegisterPath, &PreScriptPath, &LogPath}
}

// savePathGlobals saves all path globals and returns the restore function.
func savePathGlobals() func() {
	ptrs := pathGlobals()
	old := make([]string, len(ptrs))
	for i, p := range ptrs {
		old[i] = *p
	}
	return func() {
		for i, p := range ptrs {
			*p = old[i]
		}
	}
}

func TestSetupPathsLayout(t *testing.T) {
	defer savePathGlobals()()
	home := filepath.Join(string(filepath.Separator), "home", "tester")
	if runtime.GOOS == "windows" {
		home = `C:\Users\tester`
	}
	setupPaths(home, "lab1")

	inst := filepath.Join(home, "shuttle", "shuttle_lab1")
	want := map[string][2]string{
		"TempPath":          {TempPath, filepath.Join(inst, "shuttle_temp")},
		"TempConvertedPath": {TempConvertedPath, filepath.Join(inst, "shuttle_converted_temp")},
		"PreTempPath":       {PreTempPath, filepath.Join(inst, "shuttle_pre_temp")},
		"FlatTarTempPath":   {FlatTarTempPath, filepath.Join(inst, "shuttle_flat_tar_temp")},
		"IncomingPath":      {IncomingPath, filepath.Join(inst, "shuttle_incoming")},
		"ArchivePath":       {ArchivePath, filepath.Join(inst, "shuttle_archive_temp")},
		"FlatTarOutPath":    {FlatTarOutPath, filepath.Join(inst, "shuttle_flat_tar_out")},
		"RegisterPath":      {RegisterPath, filepath.Join(inst, "sent_register.json")},
		"LogPath":           {LogPath, filepath.Join(inst, "shuttle_log.txt")},
		"PreScriptPath":     {PreScriptPath, filepath.Join(home, "shuttle", "scripts")},
	}
	for name, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", name, v[0], v[1])
		}
	}
}

func TestSetupPathsNoForwardSlashOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	defer savePathGlobals()()
	setupPaths(`C:\Users\tester`, "lab1")
	for _, p := range pathGlobals() {
		if strings.Contains(*p, "/") {
			t.Errorf("path %q contains a forward slash", *p)
		}
	}
}

func TestSetupPathsWindowsNameIsCleaned(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	defer savePathGlobals()()
	setupPaths(`C:\Users\tester`, `a:b/c.`)
	want := filepath.Join(`C:\Users\tester`, "shuttle", "shuttle_a_b_c", "shuttle_temp")
	if TempPath != want {
		t.Errorf("TempPath = %q, want %q", TempPath, want)
	}
}

func TestInstanceDirName(t *testing.T) {
	if runtime.GOOS == "windows" {
		cases := map[string]string{
			"lab1":    "lab1",
			"a:b":     "a_b",
			`a/b\c`:   "a_b_c",
			"x.":      "x",
			"x. .":    "x",
			`<>:"|?*`: "_______",
			"":        "_archive",
		}
		for in, want := range cases {
			if got := instanceDirName(in); got != want {
				t.Errorf("instanceDirName(%q) = %q, want %q", in, got, want)
			}
		}
		return
	}
	// Elsewhere the name stays exactly as it is, so existing installs keep
	// their folder and their sent register.
	for _, in := range []string{"lab1", "a:b", "x.", "x ", "a?b", "", "a b"} {
		if got := instanceDirName(in); got != in {
			t.Errorf("instanceDirName(%q) = %q, want unchanged", in, got)
		}
	}
}
