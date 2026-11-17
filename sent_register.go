package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// sentEntry is what the register remembers about a file that was handed over.
type sentEntry struct {
	ModTime int64 `json:"mtime"` // UnixNano
	Size    int64 `json:"size"`
}

// sentRegisterFile is the on-disk layout of the register.
type sentRegisterFile struct {
	Version int                  `json:"version"`
	Files   map[string]sentEntry `json:"files"`
}

// SentRegister remembers which files (by "/"-separated path relative to -src,
// modification time and size) were already handed over to the upload pipeline.
// It is used by a single goroutine (the process manager) only.
type SentRegister struct {
	path    string               // "" = in memory only (tests)
	entries map[string]sentEntry // key: "/"-separated path relative to -src
	dirty   bool
}

// newSentRegister returns an empty in-memory register.
func newSentRegister() *SentRegister {
	return &SentRegister{entries: make(map[string]sentEntry)}
}

// LoadSentRegister reads the register at path. A missing file gives an empty
// register. An unreadable or corrupt file is logged, renamed to
// <path>.corrupt-<unixtime> (best effort) and gives an empty register.
func LoadSentRegister(path string) *SentRegister {
	r := newSentRegister()
	r.path = path
	if path == "" {
		return r
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			ErrorLogger.Println("sent register unreadable: ", err)
			r.quarantine()
		}
		return r
	}
	var f sentRegisterFile
	if err := json.Unmarshal(data, &f); err != nil {
		ErrorLogger.Println("sent register corrupt: ", err)
		r.quarantine()
		return r
	}
	for k, v := range f.Files {
		r.entries[k] = v
	}
	return r
}

func (r *SentRegister) quarantine() {
	dst := fmt.Sprintf("%s.corrupt-%d", r.path, time.Now().Unix())
	if err := os.Rename(r.path, dst); err != nil {
		ErrorLogger.Println("cannot move away the corrupt sent register: ", err)
	}
}

// Len returns the number of entries.
func (r *SentRegister) Len() int {
	return len(r.entries)
}

// Unchanged reports whether rel is recorded with the same mtime and size.
func (r *SentRegister) Unchanged(rel string, info os.FileInfo) bool {
	e, ok := r.entries[rel]
	return ok && e.ModTime == info.ModTime().UnixNano() && e.Size == info.Size()
}

// Record remembers that the file rel (with this state) was handed over.
func (r *SentRegister) Record(rel string, info os.FileInfo) {
	e := sentEntry{ModTime: info.ModTime().UnixNano(), Size: info.Size()}
	if old, ok := r.entries[rel]; ok && old == e {
		return
	}
	r.entries[rel] = e
	r.dirty = true
}

// Prune removes entries whose key is not in seen.
func (r *SentRegister) Prune(seen map[string]bool) {
	for k := range r.entries {
		if !seen[k] {
			delete(r.entries, k)
			r.dirty = true
		}
	}
}

// Save writes the register atomically (temp file in the same folder, then
// rename). It does nothing for a clean or in-memory register. On error the
// register stays dirty, so the next pass retries.
func (r *SentRegister) Save() error {
	if !r.dirty || r.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(sentRegisterFile{Version: 1, Files: r.entries}, "", "  ")
	if err != nil {
		return err
	}
	f, err := CreateTempCompat(filepath.Dir(r.path), "sent_register-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	r.dirty = false
	return nil
}
