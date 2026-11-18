package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"

	"github.com/StarmanMartin/gowebdav"
)

// TransferManagerWebdav reacts on the channel done_files.
// If folder of file is ready to send it sends it via WebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set.
type TransferManagerWebdav struct {
	args *Args
	DestCredentials
	client *gowebdav.Client
}

// doWork runs in a endless loop. It reacts on the channel done_files.
// If folder of file is ready to send it sends it via HWebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m *TransferManagerWebdav) doWork(quit chan int) {
	doWorkImplementation(quit, m, m.srcDir, m.args.sendType, m.args.duration)
}

// sizedBody is a request body of known size. http.NewRequest only sets
// ContentLength for bytes/strings readers, so an *os.File would be sent
// chunked, which some WebDAV servers reject. sizedTransport restores the
// length. Close is a no-op: the caller owns the file.
type sizedBody struct {
	io.Reader
	size int64
}

func (b *sizedBody) Close() error { return nil }

// sizedTransport sets ContentLength for requests whose body is a *sizedBody.
type sizedTransport struct {
	base http.RoundTripper
}

func (t *sizedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if sb, ok := req.Body.(*sizedBody); ok {
		// RoundTrippers must not modify the request: send a shallow copy.
		r2 := *req
		r2.ContentLength = sb.size
		if sb.size == 0 {
			r2.Body = http.NoBody
		}
		req = &r2
	}
	return base.RoundTrip(req)
}

// connect_to_server creates a fresh client and checks the server. The client
// has no overall timeout (that would limit whole uploads); the transport
// carries the connect, TLS and response-header timeouts.
func (m *TransferManagerWebdav) connect_to_server() error {
	m.client = nil
	c := gowebdav.NewClient(m.dst.String(), m.user, m.pass, tr)
	c.SetTransport(&sizedTransport{base: tr})
	if err := c.Connect(); err != nil {
		return err
	}
	m.client = c
	return nil
}

// send_file uploads localPath to <dst>/<remoteRel>. The client root is dst.
// The client is reused; it is dropped after any error, so the next call
// reconnects.
func (m *TransferManagerWebdav) send_file(localPath, remoteRel string) (ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			err = errors.New(fmt.Sprintf("%+v", r))
			ErrorLogger.Printf("WebDav Panic: %s\n", err)
		}
		if !ok {
			m.client = nil
		}
	}()

	f, err := os.Open(localPath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}

	if m.client == nil {
		if err = m.connect_to_server(); err != nil {
			return false, err
		}
	}
	InfoLogger.Println("Sending...", remoteRel)

	if dir := path.Dir(remoteRel); dir != "." {
		if err = m.client.MkdirAll(dir, 0644); err != nil {
			return false, err
		}
	}

	err = m.client.WriteStream(remoteRel, &sizedBody{Reader: f, size: info.Size()}, 0644)
	return err == nil, err
}
