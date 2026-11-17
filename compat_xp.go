//go:build !go1.16
// +build !go1.16

package main

import (
	"io/ioutil"
	"os"
)

func ReadDirCompat(path string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(path)
}

func ReadFileCompat(path string) ([]byte, error) {
	return ioutil.ReadFile(path)
}

// CreateTempCompat creates a temporary file in dir (the OS temp dir if dir is
// empty). Never pass "*" in prefix: Go 1.10 does not expand it and "*" is not
// allowed in Windows file names. A random suffix is appended to the prefix.
func CreateTempCompat(dir, prefix string) (*os.File, error) {
	return ioutil.TempFile(dir, prefix)
}
