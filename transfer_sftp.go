//go:build go1.11
// +build go1.11

package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// TransferManagerSftp reacts on the channel done_files.
// If folder of file is ready to send it sends it via WebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set.
type TransferManagerSftp struct {
	args *Args
	DestCredentials
	conn   *ssh.Client
	client *sftp.Client
}

// doWork runs in a endless loop. It reacts on the channel done_files.
// If folder of file is ready to send it sends it via HWebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m *TransferManagerSftp) doWork(quit chan int) {
	doWorkImplementation(quit, m, m.srcDir, m.args.sendType, m.args.duration)
}

func (m *TransferManagerSftp) connect_to_server() error {
	user := m.user
	password := m.pass
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }),
	}

	dst := m.dst.Host

	if m.conn != nil {
		_ = m.conn.Close()
		_ = m.client.Close()
	}

	conn, err := ssh.Dial("tcp", dst, config)
	if err != nil {
		return err
	}

	// create new SFTP client
	client, err := sftp.NewClient(conn)
	if err != nil {
		defer conn.Close()
		return err
	}

	m.client = client
	m.conn = conn

	InfoLogger.Println("SSH Connected!")
	return nil
}

// send_file sends a file via WebDAV
func (m *TransferManagerSftp) send_file(path_to_file string, file os.FileInfo) (bool, error) {
	var webdavFilePath, urlPathDir string

	err := m.connect_to_server()
	if err != nil {
		return false, err
	}
	if m.args.sendType == "file" {
		webdavFilePath = file.Name()
	} else if relpath, err := filepath.Rel(TempPath, path_to_file); err == nil {
		webdavFilePath = strings.Replace(relpath, string(os.PathSeparator), "/", -1)
		webdavFilePath = strings.TrimPrefix(webdavFilePath, "./")
	} else {
		return false, err
	}
	webdavFilePath = filepath.Join(m.dst.Path, webdavFilePath)
	urlPathDir = filepath.Dir(webdavFilePath)
	urlPathDir = strings.Replace(urlPathDir, string(os.PathSeparator), "/", -1)
	webdavFilePath = strings.Replace(webdavFilePath, string(os.PathSeparator), "/", -1)
	InfoLogger.Println("Sending...", webdavFilePath)

	if urlPathDir != "." {
		err := m.client.MkdirAll(urlPathDir)
		if err != nil {
			return false, err
		}
	}

	srcFile, err := os.Open(path_to_file)
	if err != nil {
		return false, err
	}
	defer srcFile.Close()

	dstFile, err := m.client.OpenFile(webdavFilePath, (os.O_WRONLY | os.O_CREATE | os.O_TRUNC))
	if err != nil {
		return false, err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return false, err
	}

	return true, nil
}
