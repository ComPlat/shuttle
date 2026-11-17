package main

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProcessManager manges the file watching process.
// A file in <CMD arg -src> (or a root subdirectory, for folder/zip/tar) is
// pushed into the channel 'done_files' when it is stable (not modified for
// <CMD -duration>), not locked and not yet in the sent register with the same
// mtime and size.
type ProcessManager struct {
	args             *Args
	doneFiles        chan string
	doneFlatPrefixes map[string][]string
	register         *SentRegister
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

// readyFile is a file that belongs to a ready key, as seen by the scan.
type readyFile struct {
	rel  string // "/"-separated, relative to -src
	info os.FileInfo
}

// scanGroup collects the state of one key during the scan.
type scanGroup struct {
	files   []readyFile
	changed bool
	blocked bool // an unstable or locked file holds the group back
}

// lockCheck is a file that must be checked for a lock if its group becomes
// a send candidate.
type lockCheck struct {
	key, path string
}

// scanReady walks -src once. It returns the ready keys (file rel paths, or
// root folders for folder/zip/tar) with all files that belong to them, and
// the set of all file keys it saw. Only an error at the -src root is returned;
// errors on single entries are logged and the entry is skipped.
func (m ProcessManager) scanReady(now time.Time) (map[string][]readyFile, map[string]bool, error) {
	perFile := m.args.sendType == "file" || m.args.sendType == "flat_tar"
	groups := make(map[string]*scanGroup)
	seen := make(map[string]bool)
	var toCheck []lockCheck
	err := filepath.Walk(m.args.src,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if path == m.args.src {
					return err
				}
				ErrorLogger.Println(err)
				return nil
			}
			if info == nil || info.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(m.args.src, path)
			if err != nil {
				ErrorLogger.Println(err)
				return nil
			}
			rel := filepath.ToSlash(relPath)
			seen[rel] = true
			key := rel
			if !perFile {
				key = getRootDir(relPath)
			}
			g := groups[key]
			if g == nil {
				g = &scanGroup{}
				groups[key] = g
			}
			g.files = append(g.files, readyFile{rel: rel, info: info})
			changed := !m.register.Unchanged(rel, info)
			if changed {
				g.changed = true
			}
			if perFile && !changed {
				return nil
			}
			if now.Sub(info.ModTime()) <= m.args.duration {
				g.blocked = true
			} else {
				toCheck = append(toCheck, lockCheck{key: key, path: path})
			}
			return nil
		})
	if err != nil {
		return map[string][]readyFile{}, seen, err
	}
	// The lock check renames a file onto itself. Only do that for files of
	// groups that would otherwise be sent, never for unchanged folders: a
	// lab PC's whole source tree must not be touched on every pass.
	for _, c := range toCheck {
		g := groups[c.key]
		if !g.changed || g.blocked {
			continue
		}
		if m.isFileLocked(c.path) {
			g.blocked = true
		}
	}
	ready := make(map[string][]readyFile)
	for key, g := range groups {
		if g.changed && !g.blocked {
			ready[key] = g.files
		}
	}
	return ready, seen, nil
}

// doWork runs in an endless loop. It watches the files in the <CMD arg -src> directory.
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m ProcessManager) doWork(quit chan int) {
	InfoLogger.Println("Started watch process.")
	for {
		select {
		case <-quit:
			return
		default:
			now := time.Now()
			ready, seen, err := m.scanReady(now)

			if err != nil {
				ErrorLogger.Println(err)
			}

			if m.args.sendType == "flat_tar" {
				m.collectTarPrefixes()
			}

			keys := make([]string, 0, len(ready))
			for k := range ready {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				InfoLogger.Println("Folder/File ready to send: ", k)
				srcPath := filepath.Join(m.args.src, filepath.FromSlash(k))
				if m.args.sendType == "flat_tar" {
					base := filepath.Base(srcPath)
					dst := filepath.Join(FlatTarTempPath, uniqueName(FlatTarTempPath, base))
					if err := Copy(srcPath, dst); err != nil {
						// Not recorded: the next pass retries.
						ErrorLogger.Println(err)
						continue
					}
				} else {
					m.doneFiles <- srcPath
				}
				for _, f := range ready[k] {
					m.register.Record(f.rel, f.info)
				}
			}
			// A failed scan of the root (e.g. a network drive that is gone)
			// must not wipe the register.
			if err == nil {
				m.register.Prune(seen)
			}
			if err := m.register.Save(); err != nil {
				ErrorLogger.Println("saving sent register: ", err)
			}
			if m.args.sendType == "flat_tar" {
				m.processTarPrefixes()
			}

			time.Sleep(m.args.duration - time.Since(now))
		}
	}
}

// newProcessManager factory for ProcessManager struct
// A nil register gives an empty in-memory one.
func newProcessManager(args *Args, doneFiles chan string, register *SentRegister) ProcessManager {
	if register == nil {
		register = newSentRegister()
	}
	return ProcessManager{args: args, doneFiles: doneFiles, doneFlatPrefixes: make(map[string][]string), register: register}
}
