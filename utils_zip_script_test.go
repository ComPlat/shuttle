package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// zipTmpLeftovers lists entries in dir whose name starts with prefix.
func zipTmpLeftovers(t *testing.T, dir, prefix string) []string {
	t.Helper()
	entries, err := ReadDirCompat(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestAddBytesToZip(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	zp, err := zipFolder(utilsArchiveFixture(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err := AddBytesToZip(zp, "n.txt", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	want := utilsWantEntries()
	want["n.txt"] = "payload"
	utilsCheckEntries(t, readZipEntries(t, zp), want)
	if l := zipTmpLeftovers(t, filepath.Dir(zp), "ziptmp-"); len(l) != 0 {
		t.Errorf("temp zips left behind: %v", l)
	}
}

func TestAddBytesToZipFailureKeepsOriginal(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	bad := filepath.Join(root, "bad.zip")
	orig := []byte("this is not a zip file")
	if err := ioutil.WriteFile(bad, orig, 0644); err != nil {
		t.Fatal(err)
	}
	if err := AddBytesToZip(bad, "x", []byte("y")); err == nil {
		t.Fatal("expected an error for an invalid zip")
	}
	got, err := ioutil.ReadFile(bad)
	if err != nil || !bytes.Equal(got, orig) {
		t.Errorf("original changed: %q err=%v", got, err)
	}
	if l := zipTmpLeftovers(t, root, "ziptmp-"); len(l) != 0 {
		t.Errorf("temp zips left behind: %v", l)
	}

	missing := filepath.Join(root, "missing.zip")
	if err := AddBytesToZip(missing, "x", []byte("y")); err == nil {
		t.Error("expected an error for a missing zip")
	}
	if l := zipTmpLeftovers(t, root, "ziptmp-"); len(l) != 0 {
		t.Errorf("temp zips left behind: %v", l)
	}
}

func TestAddFileToZipMissingFileKeepsOriginal(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	zp, err := zipFolder(utilsArchiveFixture(t, root))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := ioutil.ReadFile(zp)
	if err := AddFileToZip(zp, filepath.Join(root, "nope"), "n"); err == nil {
		t.Fatal("expected an error")
	}
	after, _ := ioutil.ReadFile(zp)
	if !bytes.Equal(before, after) {
		t.Error("zip changed although adding failed")
	}
}

func TestAddLinkToZipLeavesNoTempFiles(t *testing.T) {
	oldPublic := args.public
	defer func() { args.public = oldPublic }()
	args.public = "https://example.org/"

	root, cleanup := newTempDir(t)
	defer cleanup()
	zp, err := zipFolder(utilsArchiveFixture(t, root))
	if err != nil {
		t.Fatal(err)
	}
	before := len(zipTmpLeftovers(t, os.TempDir(), "link-"))
	if err := AddLinkToZip(filepath.Join(root, "f.bin"), zp); err != nil {
		t.Fatal(err)
	}
	if after := len(zipTmpLeftovers(t, os.TempDir(), "link-")); after != before {
		t.Errorf("link-* files in os.TempDir(): before %d, after %d", before, after)
	}
	if l := zipTmpLeftovers(t, filepath.Dir(zp), "ziptmp-"); len(l) != 0 {
		t.Errorf("temp zips left behind: %v", l)
	}
}

func TestReplaceFile(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	writeTestFile(t, src, "new")
	writeTestFile(t, dst, "old")
	if err := replaceFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := ioutil.ReadFile(dst); string(b) != "new" {
		t.Errorf("dst = %q; want new", b)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone (err=%v)", err)
	}
	// Missing source: error, destination untouched.
	if err := replaceFile(filepath.Join(root, "none"), dst); err == nil {
		t.Error("expected an error for a missing source")
	}
	if b, _ := ioutil.ReadFile(dst); string(b) != "new" {
		t.Errorf("dst changed on failure: %q", b)
	}
}

func TestIsRunnableScript(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	win := runtime.GOOS == "windows"

	type tc struct {
		name string
		file string // file name; created as a regular file unless dir
		dir  bool
		mode os.FileMode
		want bool
	}
	cases := []tc{
		{"plain data file", "data.txt", false, 0644, false},
		{"dir named x.exe", "dir.exe", true, 0755, false},
		{"hidden exe", ".hidden.exe", false, 0755, false},
		{"hidden exec bit", ".hidden", false, 0755, false},
	}
	if win {
		cases = append(cases,
			tc{"exe", "a.exe", false, 0644, true},
			tc{"EXE upper", "B.EXE", false, 0644, true},
			tc{"mixed case", "c.ExE", false, 0644, true},
			tc{"sh is not run", "d.sh", false, 0644, false},
			tc{"bat is not run", "e.bat", false, 0644, false},
			tc{"cmd is not run", "f.cmd", false, 0644, false},
			tc{"exe suffix only", "gexe", false, 0644, false},
			tc{"exe.txt", "h.exe.txt", false, 0644, false},
		)
	} else {
		cases = append(cases,
			tc{"sh with exec", "a.sh", false, 0755, true},
			tc{"no extension with exec", "prog", false, 0755, true},
			tc{"fish with exec", "b.fish", false, 0700, true},
			tc{"group exec only", "c.bin", false, 0610, true},
			tc{"exe name with exec", "d.exe", false, 0755, true},
			tc{"sh without exec", "e.sh", false, 0644, false},
			tc{"exe without exec", "f.exe", false, 0644, false},
		)
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(root, c.file)
			if c.dir {
				if err := os.Mkdir(p, 0755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, p, "x")
				if !win {
					if err := os.Chmod(p, c.mode); err != nil {
						t.Fatal(err)
					}
				}
			}
			info, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := isRunnableScript(info); got != c.want {
				t.Errorf("isRunnableScript(%s, mode %v) = %v; want %v", c.file, info.Mode(), got, c.want)
			}
		})
	}
}

func TestIsRunnableScriptSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root, cleanup := newTempDir(t)
	defer cleanup()
	target := filepath.Join(root, "t.sh")
	writeTestFile(t, target, "x")
	if err := os.Chmod(target, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "l.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if isRunnableScript(info) {
		t.Error("an unresolved symlink entry is not a regular file and must not run")
	}
	if isRunnableScript(nil) {
		t.Error("nil info must not run")
	}

	// RunPreScripts resolves links first: a link to an executable runs, and
	// keeps the link's name; a dangling link is skipped.
	resolved, ok := resolveScriptEntry(root, info)
	if !ok || !isRunnableScript(resolved) {
		t.Errorf("a link to an executable must be runnable after resolving (ok=%v)", ok)
	}
	if ok && resolved.Name() != "l.sh" {
		t.Errorf("resolved name = %q, want the link name l.sh", resolved.Name())
	}
	dangling := filepath.Join(root, "dangling.sh")
	if err := os.Symlink(filepath.Join(root, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	dinfo, err := os.Lstat(dangling)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveScriptEntry(root, dinfo); ok {
		t.Error("a dangling link must be skipped")
	}
}
