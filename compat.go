//go:build go1.16
// +build go1.16

package main

import "os"

func ReadDirCompat(path string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var infos []os.FileInfo
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func ReadFileCompat(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// CreateTempCompat creates a temporary file in dir (the OS temp dir if dir is
// empty). Never pass "*" in prefix: Go 1.10 does not expand it and "*" is not
// allowed in Windows file names. A random suffix is appended to the prefix.
func CreateTempCompat(dir, prefix string) (*os.File, error) {
	return os.CreateTemp(dir, prefix)
}
