package main

import (
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
	args             *Args
	doneFiles        chan string
	doneFlatPrefixes map[string][]string
}

func (m ProcessManager) applyRegex(name string) ([]string, string) {
	res := m.args.commonRegex.FindStringSubmatch(name)
	if res == nil {
		res = make([]string, 1)
		res[0] = name + "_archive"
	}
	if len(res) > 1 {
		res = res[1:]
	}
	joinedRes := strings.Join(res, "___")
	if len(joinedRes) == 0 || joinedRes == name {
		joinedRes = name + "_archive"
	}

	return res, joinedRes
}

func (m ProcessManager) collectTarPrefixes() {
	entries, err := ReadDirCompat(FlatTarTempPath)
	if err != nil {
		log.Fatal(err)
	}
	for prefix := range m.doneFlatPrefixes {
		delete(m.doneFlatPrefixes, prefix)
	}

	for _, v := range entries {
		if v.IsDir() {
			continue
		}
		res, joinedRes := m.applyRegex(v.Name())
		m.doneFlatPrefixes[joinedRes] = res
	}
}

func (m ProcessManager) processTarPrefixes() {
	entries, err := ReadDirCompat(FlatTarTempPath)
	if err != nil {
		log.Fatal(err)
	}

	for hashPrefix, groups := range m.doneFlatPrefixes {
		dirName := filepath.Join(FlatTarTempPath, safeDirName(hashPrefix))
		err := os.MkdirAll(dirName, os.ModeDir|os.ModePerm)
		if err != nil {
			ErrorLogger.Println(err)
			continue
		}
		var moved []string
		for _, v := range entries {
			if v.IsDir() {
				continue
			}

			res, _ := m.applyRegex(v.Name())

			if AreEqual(res, groups) {
				sourcePath := filepath.Join(FlatTarTempPath, v.Name())
				dstPath := filepath.Join(dirName, v.Name())
				if err := Copy(sourcePath, dstPath); err != nil {
					ErrorLogger.Println(err)
					continue
				}
				if err := os.Remove(sourcePath); err != nil {
					// Keep the file where it is; do not archive it twice.
					ErrorLogger.Println(err)
					_ = os.Remove(dstPath)
					continue
				}
				moved = append(moved, v.Name())
			}
		}

		if len(moved) == 0 {
			_ = os.Remove(dirName)
			continue
		}

		tarPath, err := tarFolder(dirName)
		if err != nil {
			ErrorLogger.Println(err)
			// Nothing is sent. Move the sources back so the next cycle retries.
			for _, name := range moved {
				back := filepath.Join(FlatTarTempPath, name)
				if err := os.Rename(filepath.Join(dirName, name), back); err != nil {
					ErrorLogger.Println(err)
				}
			}
			_ = os.Remove(dirName)
			continue
		}

		if err := os.RemoveAll(dirName); err != nil {
			ErrorLogger.Println(err)
		}
		// Finished archives live outside FlatTarTempPath, so they are never archived again.
		outPath := filepath.Join(FlatTarOutPath, uniqueName(FlatTarOutPath, filepath.Base(tarPath)))
		if err := os.Rename(tarPath, outPath); err != nil {
			// Keep the tar for inspection and send nothing.
			ErrorLogger.Println(err)
			delete(m.doneFlatPrefixes, hashPrefix)
			continue
		}
		m.doneFiles <- outPath

		delete(m.doneFlatPrefixes, hashPrefix)
	}
}

// fileLockedFn reports whether the file at path is locked by another process.
// It is a variable so that tests can inject a fake.
var fileLockedFn = func(path string) bool {
	return os.Rename(path, path) != nil
}

// isFileLocked only has an effect on Windows: renaming a file onto itself
// fails there while another process holds it open, but succeeds on Linux.
func (m ProcessManager) isFileLocked(path string) bool {
	return fileLockedFn(path)
}

// scanReady walks <CMD arg -src> once and returns, per file or root folder, whether it is
// ready to be sent (true) or was modified within the last <duration> (false).
func (m ProcessManager) scanReady(now time.Time, lockedFiles map[string]struct{}) (map[string]bool, error) {
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
					// A locked file holds back its whole root folder.
					if m.args.sendType != "file" && m.args.sendType != "flat_tar" {
						if relPath, err := filepath.Rel(m.args.src, path); err == nil {
							doneFolders[getRootDir(relPath)] = false
						} else {
							ErrorLogger.Println(err)
						}
					}
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
	return doneFolders, err
}

// doWork runs in an endless loop. It watches the files in the <CMD arg -src> directory.
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
			doneFolders, err := m.scanReady(now, lockedFiles)

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
						base := filepath.Base(k)
						dst := filepath.Join(FlatTarTempPath, uniqueName(FlatTarTempPath, base))
						err := Copy(src, dst)
						if err != nil {
							ErrorLogger.Println(err)
						}
					} else {
						m.doneFiles <- filepath.Join(m.args.src, k)
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
func newProcessManager(args *Args, doneFiles chan string) ProcessManager {
	return ProcessManager{args: args, doneFiles: doneFiles, doneFlatPrefixes: make(map[string][]string)}
}
