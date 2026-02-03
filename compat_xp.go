//go:build !go1.11
// +build !go1.11

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
