package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// TransferManagerChemAssistent uploads files to a ChemAssistentManager job
// with one multipart POST per request (one request = one run). dst is the
// full trigger URL, pass is the API key.
type TransferManagerChemAssistent struct {
	args *Args
	DestCredentials
	client *http.Client // reused; nil = create on next use
}

func (m *TransferManagerChemAssistent) doWork(quit chan int) {
	doWorkImplementation(quit, m, m.srcDir, m.args.sendType, m.args.duration)
}

// ensureClient returns the reused client. It has no overall timeout, because
// that would cut off big uploads; the transport carries the other timeouts.
func (m *TransferManagerChemAssistent) ensureClient() *http.Client {
	if m.client == nil {
		var rt http.RoundTripper = http.DefaultTransport
		if tr != nil {
			rt = tr
		}
		m.client = &http.Client{Transport: rt}
	}
	return m.client
}

func (m *TransferManagerChemAssistent) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+m.pass)
}

// connect_to_server creates a fresh client and, for the standard trigger
// path, checks the server and the key with GET .../api/jobs/runs/0
// (404 = reachable and key valid, 401 = key refused).
func (m *TransferManagerChemAssistent) connect_to_server() error {
	m.client = nil
	c := m.ensureClient()
	u := m.dst
	if !strings.HasSuffix(u.Path, chemAssistentTriggerPath) {
		return nil
	}
	prefix := strings.TrimSuffix(u.Path, chemAssistentTriggerPath)
	u.Path = prefix + "/api/jobs/runs/0"
	u.RawPath = ""
	u.RawQuery = ""
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}
	m.authorize(req)
	resp, err := c.Do(req)
	if err != nil {
		m.client = nil
		return fmt.Errorf("%s", redactSecrets(err.Error(), m.pass))
	}
	_, _ = io.Copy(ioutil.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("ChemAssistentManager refused the API key (401)")
	}
	return fmt.Errorf("ChemAssistentManager check failed with status %d", resp.StatusCode)
}

// uploadPart is one file of a multipart upload.
type uploadPart struct {
	name, path string
}

// flattenName turns a relative path into a single file name: "/" becomes
// "__", control characters and quotes become "_".
func flattenName(rel string) string {
	rel = strings.Replace(rel, "\\", "/", -1)
	rel = strings.Replace(rel, "/", "__", -1)
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return '_'
		}
		return r
	}, rel)
}

// uniqueNames makes names unique by inserting _2, _3, ... before the
// extension.
func uniqueNames(names []string) []string {
	seen := make(map[string]bool)
	out := make([]string, len(names))
	for i, n := range names {
		cand := n
		if seen[cand] {
			ext := filepath.Ext(n)
			base := strings.TrimSuffix(n, ext)
			for k := 2; ; k++ {
				cand = fmt.Sprintf("%s_%d%s", base, k, ext)
				if !seen[cand] {
					break
				}
			}
		}
		seen[cand] = true
		out[i] = cand
	}
	return out
}

// buildUpload builds a multipart body of known length: part headers are
// written into a buffer, the file contents are streamed. The caller must
// call closeAll when done.
func buildUpload(files []uploadPart, message string) (body io.Reader, contentType string, length int64, closeAll func(), err error) {
	var opened []*os.File
	closeAll = func() {
		for _, f := range opened {
			f.Close()
		}
	}
	var sizes []int64
	for _, p := range files {
		f, oerr := os.Open(p.path)
		if oerr != nil {
			closeAll()
			return nil, "", 0, func() {}, oerr
		}
		opened = append(opened, f)
		st, serr := f.Stat()
		if serr != nil {
			closeAll()
			return nil, "", 0, func() {}, serr
		}
		sizes = append(sizes, st.Size())
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	var readers []io.Reader
	for i, p := range files {
		if _, cerr := mw.CreateFormFile("file", p.name); cerr != nil {
			closeAll()
			return nil, "", 0, func() {}, cerr
		}
		seg := append([]byte(nil), buf.Bytes()...)
		buf.Reset()
		length += int64(len(seg)) + sizes[i]
		readers = append(readers, bytes.NewReader(seg), io.LimitReader(opened[i], sizes[i]))
	}
	if werr := mw.WriteField("message", message); werr != nil {
		closeAll()
		return nil, "", 0, func() {}, werr
	}
	if cerr := mw.Close(); cerr != nil {
		closeAll()
		return nil, "", 0, func() {}, cerr
	}
	tail := append([]byte(nil), buf.Bytes()...)
	length += int64(len(tail))
	readers = append(readers, bytes.NewReader(tail))
	return io.MultiReader(readers...), mw.FormDataContentType(), length, closeAll, nil
}

func (m *TransferManagerChemAssistent) maxBytes() int64 {
	mb := m.args.maxUploadMB
	if mb <= 0 {
		mb = chemAssistentDefaultMaxMB
	}
	return mb << 20
}

// upload sends all files as one request and returns true only for 202.
func (m *TransferManagerChemAssistent) upload(files []uploadPart, message string) (bool, error) {
	var total int64
	for _, p := range files {
		st, err := os.Stat(p.path)
		if err != nil {
			return false, err
		}
		total += st.Size()
	}
	if total > m.maxBytes() {
		return false, fmt.Errorf("upload of %d bytes exceeds the limit of %d MB (-maxUpload); nothing was sent", total, m.maxBytes()>>20)
	}
	body, ctype, length, closeAll, err := buildUpload(files, message)
	if err != nil {
		return false, err
	}
	defer closeAll()
	req, err := http.NewRequest("POST", m.dst.String(), body)
	if err != nil {
		return false, err
	}
	req.ContentLength = length
	req.Header.Set("Content-Type", ctype)
	m.authorize(req)
	resp, err := m.ensureClient().Do(req)
	if err != nil {
		m.client = nil
		return false, fmt.Errorf("%s", redactSecrets(err.Error(), m.pass))
	}
	defer resp.Body.Close()
	data, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusAccepted {
		return true, nil
	}
	msg := strings.TrimSpace(string(data))
	var parsed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &parsed) == nil && parsed.Error != "" {
		msg = parsed.Error
	}
	return false, fmt.Errorf("%s", redactSecrets(fmt.Sprintf("ChemAssistentManager refused the upload (%d): %s", resp.StatusCode, msg), m.pass))
}

func (m *TransferManagerChemAssistent) send_file(localPath, remoteRel string) (bool, error) {
	parts := []uploadPart{uploadPart{flattenName(remoteRel), localPath}}
	return m.upload(parts, "Shuttle "+m.args.name+": "+remoteRel)
}

// send_folder sends all files of a folder as one request (one run).
func (m *TransferManagerChemAssistent) send_folder(srcDir, folder string, files []string) (bool, error) {
	var names []string
	for _, f := range files {
		rel, err := remoteRelPath(srcDir, f)
		if err != nil {
			return false, err
		}
		names = append(names, flattenName(rel))
	}
	names = uniqueNames(names)
	var parts []uploadPart
	for i, f := range files {
		parts = append(parts, uploadPart{names[i], f})
	}
	return m.upload(parts, "Shuttle "+m.args.name+": folder "+filepath.Base(folder))
}
