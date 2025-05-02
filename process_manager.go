package main

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProcessManager manges the file watching process.
// As soon as all files in a subdirectory of <CMD arg -src>
// (or a file directly in <CMD arg -src>) are
// not changed for almost exactly <CMD -duration> seconds,
// the subdirectory will be pushed into the channel 'done_files'.
type ProcessManager struct {
	args               *Args
	done_files         chan string
	done_flat_prefixes map[string][]string
}

func (m ProcessManager) applyRegex(name string) ([]string, string) {
	res := m.args.commonRegex.FindStringSubmatch(name)
	if res == nil {
		res = make([]string, 1, 1)
		res[0] = name + "_archive"
	}
	if len(res) > 1 {
		res = res[1:]
	}
	joined_res := strings.Join(res, "___")
	if len(joined_res) == 0 || joined_res == name {
		joined_res = name + "_archive"
	}

	return res, joined_res
}

func (m ProcessManager) collectTarPrefixes() {
	entries, err := ioutil.ReadDir(FlatTarTempPath)
	if err != nil {
		log.Fatal(err)
	}
	for prefix := range m.done_flat_prefixes {
		delete(m.done_flat_prefixes, prefix)
	}

	for _, v := range entries {
		if v.IsDir() {
			continue
		}
		res, joined_res := m.applyRegex(v.Name())
		m.done_flat_prefixes[joined_res] = res
	}
}

func (m ProcessManager) processTarPrefixes() {
	entries, err := ioutil.ReadDir(FlatTarTempPath)
	if err != nil {
		log.Fatal(err)
	}

	for hash_prefix, groups := range m.done_flat_prefixes {
		dirName := filepath.Join(FlatTarTempPath, hash_prefix)
		err := os.MkdirAll(dirName, os.ModeDir|os.ModePerm)
		if err != nil {
			ErrorLogger.Println(err)
			continue
		}
		for _, v := range entries {
			if v.IsDir() {
				continue
			}

			res, _ := m.applyRegex(v.Name())

			if AreEqual(res, groups) {
				sourcePath := filepath.Join(FlatTarTempPath, v.Name())
				err := Copy(sourcePath, filepath.Join(dirName, v.Name()))
				if err != nil {
					ErrorLogger.Println(err)
				}
				err = os.Remove(sourcePath)
			}
		}

		tarPaht, err := tarFolder(dirName)
		if err != nil {
			ErrorLogger.Println(err)
		}

		err = Copy(tarPaht, filepath.Join(dirName, filepath.Base(tarPaht)))
		if err != nil {
			ErrorLogger.Println(err)
		}
		m.done_files <- tarPaht
		err = os.RemoveAll(dirName)
		if err != nil {
			ErrorLogger.Println(err)
			continue
		}

		delete(m.done_flat_prefixes, hash_prefix)
	}
}

func (m ProcessManager) isFileLocked(path string) bool {
	err := os.Rename(path, path)
	return err != nil
}

// doWork runs in a endless loop. It watches the files in the <CMD arg -src> directory.
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m ProcessManager) doWork(quit chan int) {
	InfoLogger.Println("Started watch process.")
	lockedFiles := make(map[string]struct{})
	for {
		select {
		case <-quit:
			return
		default:
			now := time.Now()
			doneFolders := make(map[string]bool)
			// Checking all files in <CMD arg -src>.
			err := filepath.Walk(m.args.src,
				func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if !info.IsDir() {
						modified := info.ModTime()
						diff := now.Sub(modified)
						if m.isFileLocked(path) {
							lockedFiles[path] = struct{}{}
						} else if _, exists := lockedFiles[path]; exists || diff < 2*m.args.duration {
							if exists {
								delete(lockedFiles, path)
							}
							if relPath, err := filepath.Rel(m.args.src, path); err == nil {
								folder := relPath
								if m.args.sendType != "file" && m.args.sendType != "flat_tar" {
									folder = getRootDir(relPath)
								}

								if _, ok := doneFolders[folder]; !ok {
									doneFolders[folder] = true
								}

								if diff <= m.args.duration {
									doneFolders[folder] = false
								}
							} else {
								ErrorLogger.Println(err)
							}
						}
					}
					return nil
				})

			if err != nil {
				ErrorLogger.Println(err)
			}

			// Pushing all complete subdirectory into done_files channel.
			if m.args.sendType == "flat_tar" {
				m.collectTarPrefixes()
			}

			for k, v := range doneFolders {
				if v {
					InfoLogger.Println("Folder/File ready to send: ", k)
					if m.args.sendType == "flat_tar" {
						src := filepath.Join(m.args.src, k)
						dst := filepath.Join(FlatTarTempPath, filepath.Base(k))
						err := Copy(src, dst)
						if err != nil {
							ErrorLogger.Println(err)
						}
					} else {
						m.done_files <- filepath.Join(m.args.src, k)
					}
				}
			}
			if m.args.sendType == "flat_tar" {
				m.processTarPrefixes()
			}

			time.Sleep(m.args.duration - time.Since(now))
		}
	}
}

// newProcessManager factory for ProcessManager struct
func newProcessManager(args *Args, done_files chan string) ProcessManager {
	return ProcessManager{args: args, done_files: done_files, done_flat_prefixes: make(map[string][]string)}
}
