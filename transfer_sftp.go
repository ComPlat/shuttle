// Build tag note: this file is tagged go1.11, not go1.16, on purpose. The XP
// toolchain is Go 1.10 in GOPATH mode, without golang.org/x/crypto; every
// toolchain >= 1.11 that ShuttleBuilder uses builds with modules.

//go:build go1.11
// +build go1.11

package main

import (
	"fmt"
	"io"
	"os"
	"path"

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
	// pinAlgos caches the host key algorithms that matched a bare pinned
	// fingerprint, so later connections need a single handshake.
	pinAlgos []string
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
	pinned, insecure := "", false
	if m.args != nil {
		pinned, insecure = m.args.hostKey, m.args.insecure
	}
	hostKeyCb, err := hostKeyCallback(pinned, insecure, KnownHostsPath)
	if err != nil {
		return err
	}
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKeyCb,
	}

	dst := m.dst.Host
	// Candidate host key algorithm sets, tried in order. nil = client default.
	candidates := [][]string{nil}
	switch {
	case insecure:
	case pinned == "":
		// Trust on first use: only negotiate key types already trusted for
		// this host, so a different key type is never mistaken for a change.
		if algos := knownHostKeyAlgorithms(KnownHostsPath, dst); len(algos) > 0 {
			candidates = [][]string{algos}
		}
	case pinnedHostKeyAlgorithms(pinned) != nil:
		candidates = [][]string{pinnedHostKeyAlgorithms(pinned)}
	case m.pinAlgos != nil:
		candidates = [][]string{m.pinAlgos}
	default:
		// A bare fingerprint does not say which key type it belongs to:
		// try each type until the server presents the pinned key.
		candidates = hostKeyAlgoGroups
	}

	if m.conn != nil {
		_ = m.conn.Close()
		_ = m.client.Close()
	}

	var conn *ssh.Client
	for _, algos := range candidates {
		config.HostKeyAlgorithms = algos
		conn, err = ssh.Dial("tcp", dst, config)
		if err == nil {
			if pinned != "" && !insecure && pinnedHostKeyAlgorithms(pinned) == nil {
				m.pinAlgos = algos
			}
			break
		}
	}
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

// send_file uploads localPath to <dst path>/<remoteRel>.
func (m *TransferManagerSftp) send_file(localPath, remoteRel string) (bool, error) {
	if err := m.connect_to_server(); err != nil {
		return false, err
	}
	remote := path.Join(m.dst.Path, remoteRel)
	InfoLogger.Println("Sending...", remote)

	if dir := path.Dir(remote); dir != "." {
		if err := m.client.MkdirAll(dir); err != nil {
			return false, err
		}
	}

	srcFile, err := os.Open(localPath)
	if err != nil {
		return false, err
	}
	defer srcFile.Close()
	info, err := srcFile.Stat()
	if err != nil {
		return false, err
	}

	dstFile, err := m.client.OpenFile(remote, (os.O_WRONLY | os.O_CREATE | os.O_TRUNC))
	if err != nil {
		return false, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = dstFile.Close()
		}
	}()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return false, err
	}
	closed = true
	if err = dstFile.Close(); err != nil {
		return false, err
	}

	remoteInfo, err := m.client.Stat(remote)
	if err != nil {
		return false, err
	}
	if remoteInfo.Size() != info.Size() {
		return false, fmt.Errorf("size mismatch for %s: local %d, remote %d", remote, info.Size(), remoteInfo.Size())
	}
	return true, nil
}
