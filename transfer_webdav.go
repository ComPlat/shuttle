package main

import (
	"errors"
	"fmt"
	"path"
	"time"

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

func (m *TransferManagerWebdav) connect_to_server() error {
	user := m.user
	password := m.pass

	c := gowebdav.NewClient(m.dst.String(), user, password, tr)
	c.SetTimeout(10 * time.Second)
	if err := c.Connect(); err != nil {
		return err
	}
	m.client = c
	return nil
}

// send_file uploads localPath to <dst>/<remoteRel>. The client root is dst.
func (m *TransferManagerWebdav) send_file(localPath, remoteRel string) (ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			err = errors.New(fmt.Sprintf("%+v", r))
			ErrorLogger.Printf("WebDav Panic: %s\n", err)
		}
	}()

	if err = m.connect_to_server(); err != nil {
		return false, err
	}
	InfoLogger.Println("Sending...", remoteRel)

	if dir := path.Dir(remoteRel); dir != "." {
		if err = m.client.MkdirAll(dir, 0644); err != nil {
			return false, err
		}
	}

	bytes, err := ReadFileCompat(localPath)
	if err != nil {
		return false, err
	}

	err = m.client.Write(remoteRel, bytes, 0644)
	return err == nil, err
}
