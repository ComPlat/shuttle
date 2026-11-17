package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// getRootDir returns the root directory, i.e., the first directory in the path.
// If the path is relative <root>/../<file> it returns the relative root.
// If it is not relative it returns the absolut root,  i.e.: '/'
func getRootDir(path string) string {
	current := path
	for {
		path = filepath.Dir(path)
		if path == "." || path == "" || path == current {
			return current
		}
		current = path
	}
}

func RunPreScripts(srcFilePath string) {
	entries, err := ReadDirCompat(PreScriptPath)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range entries {
		if info, ok := resolveScriptEntry(PreScriptPath, file); ok {
			runScript(info, PreScriptPath, srcFilePath)
		}
	}
}

// resolveScriptEntry follows a symlink in the scripts folder to its target,
// so an admin can link an executable into the folder. The returned info keeps
// the link's name. A dangling link is skipped.
func resolveScriptEntry(dir string, info os.FileInfo) (os.FileInfo, bool) {
	if info.Mode()&os.ModeSymlink == 0 {
		return info, true
	}
	target, err := os.Stat(filepath.Join(dir, info.Name()))
	if err != nil {
		ErrorLogger.Println("skipping script link", info.Name(), ":", err)
		return nil, false
	}
	return target, true
}

func exeDir() (string, error) {

	// DEV override
	if dir := os.Getenv("APP_BASE_DIR"); dir != "" {
		return dir, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func RunConverterScripts(srcFilePath string) {
	entries, err := exeDir()
	if err != nil {
		log.Fatal(err)
	}
	file := ""
	if runtime.GOOS == "windows" {
		file = filepath.Join(entries, "ChemConverterApp.exe")
	} else {
		file = filepath.Join(entries, "ChemConverterApp")
	}
	fileInfo, err := os.Stat(file)
	if err != nil {
		log.Fatal("Error Converter executable dose not exists:\n |->", file, "\n", err)
	}
	runScript(fileInfo, entries, "convert", "-i", srcFilePath, "-o", "jcampzip")

}

func runScript(file os.FileInfo, cwd string, args ...string) {
	if isRunnableScript(file) {
		var (
			cmd *exec.Cmd
		)
		if runtime.GOOS == "windows" {
			absScriptPath := filepath.Join(cwd, file.Name())
			cmd = exec.Command(absScriptPath, args...)
		} else {
			cmd = exec.Command("./"+file.Name(), args...)
		}

		cmd.Dir = cwd
		var outb, errb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Stderr = &errb
		err := cmd.Run()
		if err != nil {
			ErrorLogger.Println(err.Error())
		}
		InfoLogger.Println(file.Name(), "\n |-> out:", outb.String(), "\n |-> err:", errb.String())
	}
}

func AreEqual(a, b []string) bool {
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

func CopyPreTempDirectory(scrDir string) (string, error) {
	_ = os.MkdirAll(PreTempPath, os.ModePerm)
	newPath := filepath.Base(scrDir)
	err := copyDirectory(scrDir, PreTempPath)
	return filepath.Join(PreTempPath, newPath), err
}

// CopyTempDirectory hands the prepared entries over to TempPath atomically.
// Every entry is first copied completely into IncomingPath and then renamed
// into TempPath, so the transfer managers never see a half-copied entry.
// An entry whose rename is blocked (for example because the target still
// exists) stays pending in IncomingPath and is retried by the next call.
func CopyTempDirectory() error {
	_ = os.MkdirAll(IncomingPath, os.ModePerm)
	_ = os.MkdirAll(TempPath, os.ModePerm)

	// 1. Retry entries that are still pending from earlier calls.
	pending, err := ReadDirCompat(IncomingPath)
	if err != nil {
		return err
	}
	for _, p := range pending {
		from := filepath.Join(IncomingPath, p.Name())
		if err := os.Rename(from, filepath.Join(TempPath, p.Name())); err != nil {
			InfoLogger.Println("hand-over of", p.Name(), "is still pending:", err)
		}
	}

	// 2. Copy the new entries into IncomingPath and rename them into place.
	entries, err := ReadDirCompat(PreTempPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing new to hand over; pending entries were retried above.
			return nil
		}
		return err
	}
	for _, file := range entries {
		absPreTempPath := filepath.Join(PreTempPath, file.Name())
		staged := filepath.Join(IncomingPath, file.Name())
		// A pending older version is replaced by the newer, complete copy.
		if err := os.RemoveAll(staged); err != nil {
			return err
		}
		if err := copyDirectory(absPreTempPath, IncomingPath); err != nil {
			return err
		}
		if err := os.Rename(staged, filepath.Join(TempPath, file.Name())); err != nil {
			InfoLogger.Println("hand-over of", file.Name(), "is pending:", err)
		}
	}

	return os.RemoveAll(PreTempPath)
}

func copyDirectory(scrDir string, destDir string) error {
	return filepath.Walk(scrDir, func(sourcePath string, fileInfo os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		destPath, err := filepath.Rel(filepath.Dir(scrDir), sourcePath)
		if err != nil {
			return err
		}
		destPath = filepath.Join(destDir, destPath)
		if fileInfo.IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
		} else {
			if err := Copy(sourcePath, destPath); err != nil {
				return err
			}
		}
		return nil
	})
}

// Copy copies srcFile to dstFile. The source is opened first, so a missing
// source creates no destination. If anything fails after the destination was
// created, the partial destination is removed.
func Copy(srcFile, dstFile string) error {
	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer func(in *os.File) {
		if err := in.Close(); err != nil {
			ErrorLogger.Println(err)
		}
	}(in)

	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dstFile)
		return err
	}
	return nil
}

// zipFolder zips a folder and safes the zipped folder with the same name in the same directory.
// On any error the partial archive is removed and "" is returned.
func zipFolder(path_src string) (string, error) {
	output_path := path_src + ".zip"
	outFile, err := os.Create(output_path)
	if err != nil {
		return "", err
	}

	// Create a new zip archive.
	w := zip.NewWriter(outFile)
	err = filepath.Walk(path_src,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			rel_path, err := filepath.Rel(path_src, path)
			if err != nil {
				return err
			}

			dat, err := ReadFileCompat(path)
			if err != nil {
				return err
			}

			f, err := w.Create(filepath.ToSlash(rel_path))
			if err != nil {
				return err
			}
			_, err = f.Write([]byte(dat))

			return err

		})

	// Close in order (zip, then file) and keep the first error.
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if cerr := outFile.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(output_path)
		return "", err
	}
	return output_path, nil
}

// tarFolder tars and gzips a folder and saves it as <folder>.tar.gz in the same directory.
// On any error the partial archive is removed and "" is returned.
func tarFolder(path_src string) (string, error) {
	output_path := path_src + ".tar.gz"
	outFile, err := os.Create(output_path)
	if err != nil {
		return "", err
	}

	// These writers are chained: tar -> gzip -> file.
	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	err = filepath.Walk(path_src,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			rel_path, err := filepath.Rel(path_src, path)
			if err != nil {
				return err
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}

			// Get FileInfo about our file providing file size, mode, etc.
			finfo, err := file.Stat()
			if err != nil {
				_ = file.Close()
				return err
			}

			// Create a tar Header from the FileInfo data
			header, err := tar.FileInfoHeader(finfo, finfo.Name())
			if err != nil {
				_ = file.Close()
				return err
			}

			// Use full path as name (FileInfoHeader only takes the basename)
			// If we don't do this the directory strucuture would
			// not be preserved
			// https://golang.org/src/archive/tar/common.go?#L626
			header.Name = filepath.ToSlash(rel_path)

			err = tw.WriteHeader(header)
			if err == nil {
				_, err = io.Copy(tw, file)
			}
			if cerr := file.Close(); err == nil {
				err = cerr
			}
			return err
		})

	// Close in order (tar, gzip, file) and keep the first error.
	if cerr := tw.Close(); err == nil {
		err = cerr
	}
	if cerr := gw.Close(); err == nil {
		err = cerr
	}
	if cerr := outFile.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(output_path)
		return "", err
	}
	return output_path, nil
}

// safeDirName turns a name built from regex groups into a valid folder name
// on every platform: the characters < > : " / \ | ? * and control characters
// become "_", trailing dots and spaces are trimmed (Windows forbids them) and
// an empty result becomes "_archive".
func safeDirName(name string) string {
	b := []rune(name)
	for i, r := range b {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			b[i] = '_'
		default:
			if r < 0x20 || r == 0x7f {
				b[i] = '_'
			}
		}
	}
	res := strings.TrimRight(string(b), ". ")
	if res == "" {
		return "_archive"
	}
	return res
}

// uniqueName returns base if dir/base does not exist, otherwise
// <stem>_dupN<ext> with the smallest free N >= 1. ext is everything after
// the first dot of base, so "a.tar.gz" becomes "a_dup1.tar.gz".
func uniqueName(dir, base string) string {
	if _, err := os.Lstat(filepath.Join(dir, base)); os.IsNotExist(err) {
		return base
	}
	stem, ext := base, ""
	if i := strings.Index(base, "."); i >= 0 {
		stem, ext = base[:i], base[i:]
	}
	for n := 1; ; n++ {
		cand := stem + "_dup" + strconv.Itoa(n) + ext
		if _, err := os.Lstat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand
		}
	}
}

// AddLinkToZip adds the entry ".link" (public URL + base name of file) to the
// zip. No temp file is involved.
func AddLinkToZip(file, zipPath string) error {
	return AddBytesToZip(zipPath, ".link", []byte(args.public+filepath.Base(file)))
}

// AddFileToZip adds the file filePath as entry fileName to the zip.
func AddFileToZip(zipPath, filePath, fileName string) error {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}
	return AddBytesToZip(zipPath, fileName, data)
}

// AddBytesToZip rewrites the zip at zipPath with all its entries plus a new
// entry name holding data. The new zip is built in the folder of zipPath (same
// volume, so no cross-device rename) and everything is closed before it
// replaces the original. On any error the temp file is removed and the
// original zip stays intact.
func AddBytesToZip(zipPath, name string, data []byte) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	tmpFile, err := CreateTempCompat(filepath.Dir(zipPath), "ziptmp-")
	if err != nil {
		_ = r.Close()
		return err
	}
	tmpName := tmpFile.Name()
	w := zip.NewWriter(tmpFile)

	err = copyZipEntries(w, r)
	if err == nil {
		var dst io.Writer
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetModTime(time.Now())
		if dst, err = w.CreateHeader(hdr); err == nil {
			_, err = dst.Write(data)
		}
	}

	// Close in order (reader, zip writer, file) and keep the first error.
	// All of this must happen before the rename: Windows cannot rename over
	// or remove an open file.
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if cerr := tmpFile.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = replaceFile(tmpName, zipPath)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// copyZipEntries copies every entry of r into w. Each entry reader is closed
// before the function returns.
func copyZipEntries(w *zip.Writer, r *zip.ReadCloser) error {
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		hdr := f.FileHeader
		hdr.Method = zip.Deflate
		dst, err := w.CreateHeader(&hdr)
		if err == nil {
			_, err = io.Copy(dst, rc)
		}
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// replaceFile moves src over dst. If the rename fails and dst exists (on
// Windows, antivirus or indexer handles can block replacing a file), dst is
// removed and the rename is retried. In that fallback there is a short window
// in which dst does not exist; src is never touched on failure.
func replaceFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	// Only fall back when the source exists and the target is in the way.
	if _, serr := os.Lstat(src); serr != nil {
		return err
	}
	if _, serr := os.Lstat(dst); serr != nil {
		return err
	}
	if rerr := os.Remove(dst); rerr != nil {
		return err
	}
	return os.Rename(src, dst)
}

// isRunnableScript reports whether info is a script or program that
// RunPreScripts / RunConverterScripts may execute. On Windows that is a
// regular file with the extension .exe (any case); elsewhere any regular file
// with an execute bit. Directories, non-regular files and hidden files
// (leading dot) never qualify.
func isRunnableScript(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || strings.HasPrefix(info.Name(), ".") {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Ext(info.Name()), ".exe")
	}
	return info.Mode()&0111 != 0
}
