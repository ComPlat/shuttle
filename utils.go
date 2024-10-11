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
	"strings"
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

func RunPreScripts(filePath string) {
	entries, err := ioutil.ReadDir(PreScriptPath)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range entries {
		if !file.IsDir() && strings.Contains(".exe.sh.fish", filepath.Ext(file.Name())) {
			var (
				cmd *exec.Cmd
			)
			if runtime.GOOS == "windows" {
				absScriptPath := filepath.Join(PreScriptPath, file.Name())
				cmd = exec.Command(absScriptPath, filePath)
			} else {
				cmd = exec.Command("./"+file.Name(), filePath)
			}

			cmd.Dir = PreScriptPath
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
}

func CopyPreTempDirectory(scrDir string) (string, error) {
	_ = os.MkdirAll(PreTempPath, os.ModePerm)
	newPath := filepath.Base(scrDir)
	err := copyDirectory(scrDir, PreTempPath)
	return filepath.Join(PreTempPath, newPath), err
}

func CopyTempDirectory() error {
	entries, err := ioutil.ReadDir(PreTempPath)
	if err != nil {
		return err
	}

	for _, file := range entries {
		absTempPath := filepath.Join(PreTempPath, file.Name())
		err := copyDirectory(absTempPath, TempPath)
		if err != nil {
			return err
		}
	}

	return os.RemoveAll(PreTempPath)
}

func copyDirectory(scrDir string, destDir string) error {
	return filepath.Walk(scrDir, func(sourcePath string, fileInfo os.FileInfo, e error) error {
		destPath, err := filepath.Rel(filepath.Dir(scrDir), sourcePath)
		if err != nil {
			return err
		}
		destPath = filepath.Join(destDir, destPath)
		if fileInfo.IsDir() {
			_ = os.MkdirAll(destPath, 0755)
		} else {
			if err := Copy(sourcePath, destPath); err != nil {
				return err
			}
		}
		return nil
	})
}

func Copy(srcFile, dstFile string) error {
	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	defer func(out *os.File) {
		if err := out.Close(); err != nil {
			ErrorLogger.Println(err)
		}
	}(out)

	in, err := os.Open(srcFile)
	defer func(in *os.File) {
		if err := in.Close(); err != nil {
			ErrorLogger.Println(err)
		}
	}(in)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}

// zipFolder zips a folder and safes the zipped folder with the same name in the same directory.
func zipFolder(path_src string) (string, error) {
	// Create a buffer to write our archive to.
	output_path := path_src + ".zip"
	outFile, err := os.Create(output_path)
	if err != nil {
		return "", err
	}
	defer func(outFile *os.File) {
		err := outFile.Close()
		if err != nil {
			ErrorLogger.Println(err)
		}
	}(outFile)

	// Create a new zip archive.
	w := zip.NewWriter(outFile)
	defer func(w *zip.Writer) {
		err := w.Close()
		if err != nil {
			ErrorLogger.Println(err)
		}
	}(w)
	err = filepath.Walk(path_src,
		func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return err
			}

			rel_path, err := filepath.Rel(path_src, path)
			if err != nil {
				return err
			}

			dat, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}

			f, err := w.Create(rel_path)
			if err != nil {
				return err
			}
			_, err = f.Write([]byte(dat))

			return err

		})

	// Make sure to check the error on Close.

	if err != nil {
		return "", err
	}
	return output_path, nil
}

// tarFolder zips a folder and safes the zipped folder with the same name in the same directory.
func tarFolder(path_src string) (string, error) {
	// Create a buffer to write our archive to.
	output_path := path_src + ".tar.gz"
	outFile, err := os.Create(output_path)
	if err != nil {
		return "", err
	}
	defer func(outFile *os.File) {
		err := outFile.Close()
		if err != nil {
			ErrorLogger.Println(err)
		}
	}(outFile)

	// Create new Writers for gzip and tar
	// These writers are chained. Writing to the tar writer will
	// write to the gzip writer which in turn will write to
	// the "buf" writer
	gw := gzip.NewWriter(outFile)
	defer func(gwEnd *gzip.Writer) {
		err := gwEnd.Close()
		if err != nil {
			ErrorLogger.Println(err)
		}
	}(gw)
	tw := tar.NewWriter(gw)
	defer func(twEnd *tar.Writer) {
		err := twEnd.Close()
		if err != nil {
			ErrorLogger.Println(err)
		}
	}(tw)

	err = filepath.Walk(path_src,
		func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return err
			}

			rel_path, err := filepath.Rel(path_src, path)
			if err != nil {
				return err
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}

			defer func(file *os.File) {
				if file.Close() != nil {
					return
				}
			}(file)

			// Get FileInfo about our file providing file size, mode, etc.
			finfo, err := file.Stat()
			if err != nil {
				return err
			}

			// Create a tar Header from the FileInfo data
			header, err := tar.FileInfoHeader(finfo, finfo.Name())
			if err != nil {
				return err
			}

			// Use full path as name (FileInfoHeader only takes the basename)
			// If we don't do this the directory strucuture would
			// not be preserved
			// https://golang.org/src/archive/tar/common.go?#L626
			header.Name = rel_path

			// Write file header to the tar archive
			err = tw.WriteHeader(header)
			if err != nil {
				return err
			}

			// Copy file content to tar archive
			_, err = io.Copy(tw, file)
			if err != nil {
				return err
			}

			return nil

		})

	// Make sure to check the error on Close.

	if err != nil {
		return "", err
	}
	return output_path, nil
}
