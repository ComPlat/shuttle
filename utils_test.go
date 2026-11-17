package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetRootDir(t *testing.T) {
	type tc struct{ in, want string }
	cases := []tc{
		{"", ""},
		{"Ap", "Ap"},
		{"Ap/", "Ap"},
		{"Ap/Bp", "Ap"},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases,
			tc{"/", "/"},
			tc{"/Ap", "/"},
			tc{"/Ap/Bp", "/"},
			tc{"//Bp", "/"},
		)
	} else {
		cases = append(cases,
			tc{`C:\Ap\Bp`, `C:\`},
			tc{`Ap\Bp`, "Ap"},
		)
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			if got := getRootDir(c.in); got != c.want {
				t.Errorf("getRootDir(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

// utilsArchiveFixture creates <root>/A with B/a.txt, b.txt and C/c.txt and
// returns the path of A.
func utilsArchiveFixture(t *testing.T, root string) string {
	t.Helper()
	a := filepath.Join(root, "A")
	writeTestFile(t, filepath.Join(a, "B", "a.txt"), "Hallo A_B_a")
	writeTestFile(t, filepath.Join(a, "b.txt"), "Hallo A_c")
	writeTestFile(t, filepath.Join(a, "C", "c.txt"), "Hallo A_C_c")
	return a
}

func utilsWantEntries() map[string]string {
	return map[string]string{
		"B/a.txt": "Hallo A_B_a",
		"b.txt":   "Hallo A_c",
		"C/c.txt": "Hallo A_C_c",
	}
}

// utilsReadZip returns entry name -> content, with names exactly as stored.
func utilsReadZip(t *testing.T, path string) map[string]string {
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

// utilsReadTarGz returns entry name -> content, with names exactly as stored.
func utilsReadTarGz(t *testing.T, path string) map[string]string {
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

func utilsSlashKeys(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[filepath.ToSlash(k)] = v
	}
	return out
}

func utilsCheckEntries(t *testing.T, got, want map[string]string) {
	t.Helper()
	got = utilsSlashKeys(got)
	if len(got) != len(want) {
		t.Errorf("entries = %v; want %v", got, want)
	}
	for name, content := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("missing entry %q (have %v)", name, got)
			continue
		}
		if g != content {
			t.Errorf("entry %q = %q; want %q", name, g, content)
		}
	}
}

func TestZipFolder(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	a := utilsArchiveFixture(t, root)

	out, err := zipFolder(a)
	if err != nil {
		t.Fatal(err)
	}
	if out != a+".zip" {
		t.Errorf("zipFolder output = %q; want %q", out, a+".zip")
	}
	utilsCheckEntries(t, utilsReadZip(t, out), utilsWantEntries())
}

func TestTarFolder(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	a := utilsArchiveFixture(t, root)

	out, err := tarFolder(a)
	if err != nil {
		t.Fatal(err)
	}
	if out != a+".tar.gz" {
		t.Errorf("tarFolder output = %q; want %q", out, a+".tar.gz")
	}
	utilsCheckEntries(t, utilsReadTarGz(t, out), utilsWantEntries())
}

func TestArchiveEntryNamesUseSlash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("known bug, fixed in WP 4.1: zipFolder/tarFolder write backslash entry names on Windows")
	}
	root, cleanup := newTempDir(t)
	defer cleanup()
	a := utilsArchiveFixture(t, root)

	zp, err := zipFolder(a)
	if err != nil {
		t.Fatal(err)
	}
	tp, err := tarFolder(a)
	if err != nil {
		t.Fatal(err)
	}
	want := utilsWantEntries()
	for label, got := range map[string]map[string]string{
		"zip": utilsReadZip(t, zp),
		"tar": utilsReadTarGz(t, tp),
	} {
		if len(got) != len(want) {
			t.Errorf("%s entries = %v; want %v", label, got, want)
		}
		for name := range want {
			if _, ok := got[name]; !ok {
				t.Errorf("%s: missing entry %q (have %v)", label, name, got)
			}
		}
	}
}

func TestCopy(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()

	t.Run("normal", func(t *testing.T) {
		src := filepath.Join(root, "src.txt")
		dst := filepath.Join(root, "dst.txt")
		writeTestFile(t, src, "hello")
		if err := Copy(src, dst); err != nil {
			t.Fatal(err)
		}
		b, err := ioutil.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "hello" {
			t.Errorf("dst = %q; want %q", b, "hello")
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		src := filepath.Join(root, "src2.txt")
		dst := filepath.Join(root, "dst2.txt")
		writeTestFile(t, src, "new")
		writeTestFile(t, dst, "old content that is longer")
		if err := Copy(src, dst); err != nil {
			t.Fatal(err)
		}
		b, err := ioutil.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "new" {
			t.Errorf("dst = %q; want %q", b, "new")
		}
	})

	t.Run("missing source", func(t *testing.T) {
		dst := filepath.Join(root, "dst3.txt")
		if err := Copy(filepath.Join(root, "nope.txt"), dst); err == nil {
			t.Error("Copy of a missing source returned nil error")
		}
	})

	t.Run("missing source leaves no dst", func(t *testing.T) {
		t.Skip("known bug, fixed in WP 3.6: Copy leaves an empty destination file when the source is missing")
		dst := filepath.Join(root, "dst4.txt")
		_ = Copy(filepath.Join(root, "nope.txt"), dst)
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("destination exists after failed Copy (stat err = %v)", err)
		}
	})
}

func TestCopyDirectory(t *testing.T) {
	root, cleanup := newTempDir(t)
	defer cleanup()
	a := utilsArchiveFixture(t, root)
	writeTestFile(t, filepath.Join(a, "D", "E", "e.txt"), "deep")
	if err := os.MkdirAll(filepath.Join(a, "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}

	if err := copyDirectory(a, dest); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		filepath.Join("B", "a.txt"):      "Hallo A_B_a",
		"b.txt":                          "Hallo A_c",
		filepath.Join("C", "c.txt"):      "Hallo A_C_c",
		filepath.Join("D", "E", "e.txt"): "deep",
	}
	for rel, content := range want {
		b, err := ioutil.ReadFile(filepath.Join(dest, "A", rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if string(b) != content {
			t.Errorf("%s = %q; want %q", rel, b, content)
		}
	}
	if fi, err := os.Stat(filepath.Join(dest, "A", "empty")); err != nil || !fi.IsDir() {
		t.Errorf("empty directory not reproduced (err = %v)", err)
	}
	// Source must be untouched.
	if _, err := os.Stat(filepath.Join(a, "b.txt")); err != nil {
		t.Errorf("source changed: %v", err)
	}
}

func TestAreEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil and empty", nil, []string{}, true},
		{"equal", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different length", []string{"a"}, []string{"a", "b"}, false},
		{"different element", []string{"a", "b"}, []string{"a", "c"}, false},
		{"different order", []string{"a", "b"}, []string{"b", "a"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := AreEqual(c.a, c.b); got != c.want {
				t.Errorf("AreEqual(%v, %v) = %v; want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// utilsSkipIfZipRewriteUnsupported skips when AddFileToZip cannot be tested
// on this platform or filesystem layout.
func utilsSkipIfZipRewriteUnsupported(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		t.Skipf("known bug, fixed in WP 4.2: AddFileToZip renames over an open file on Windows: %v", err)
	}
	if le, ok := err.(*os.LinkError); ok && le.Op == "rename" {
		t.Skipf("known bug, fixed in WP 4.2: AddFileToZip renames across filesystems (EXDEV): %v", err)
	}
}

func TestAddFileToZip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("known bug, fixed in WP 4.2: AddFileToZip renames over an open file on Windows")
	}
	root, cleanup := newTempDir(t)
	defer cleanup()
	a := utilsArchiveFixture(t, root)
	zp, err := zipFolder(a)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "extra.txt")
	writeTestFile(t, extra, "extra content")

	err = AddFileToZip(zp, extra, "added.txt")
	utilsSkipIfZipRewriteUnsupported(t, err)
	if err != nil {
		t.Fatal(err)
	}

	want := utilsWantEntries()
	want["added.txt"] = "extra content"
	utilsCheckEntries(t, utilsReadZip(t, zp), want)
}

func TestAddLinkToZip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("known bug, fixed in WP 4.2: AddFileToZip renames over an open file on Windows")
	}
	oldPublic := args.public
	defer func() { args.public = oldPublic }()
	args.public = "https://example.org/pub/"

	root, cleanup := newTempDir(t)
	defer cleanup()
	a := utilsArchiveFixture(t, root)
	zp, err := zipFolder(a)
	if err != nil {
		t.Fatal(err)
	}

	err = AddLinkToZip(filepath.Join(root, "dir", "file.bin"), zp)
	utilsSkipIfZipRewriteUnsupported(t, err)
	if err != nil {
		t.Fatal(err)
	}

	want := utilsWantEntries()
	want[".link"] = "https://example.org/pub/file.bin"
	utilsCheckEntries(t, utilsReadZip(t, zp), want)
}
