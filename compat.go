//go:build go1.11
// +build go1.11

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
