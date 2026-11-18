// Build tag note: this file is tagged !go1.11, not !go1.16, on purpose. The XP
// toolchain is Go 1.10 in GOPATH mode, without golang.org/x/crypto; every
// toolchain >= 1.11 that ShuttleBuilder uses builds with modules.

//go:build !go1.11
// +build !go1.11

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// maxWinscpOutputTail is how much of WinSCP's output goes into an error.
const maxWinscpOutputTail = 2048

var hostKeyWarnOnce, hostKeyInsecureOnce sync.Once

// winscpPath returns the WinSCP.com next to the executable.
func winscpPath() (string, error) {
	ex, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate own executable: %v", err)
	}
	p := filepath.Join(filepath.Dir(ex), "WinSCP.com")
	// Windows is case-insensitive, so this also finds winscp.com.
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("WinSCP.com not found, expected at %s: %v", p, err)
	}
	return p, nil
}

// runWinscp writes lines to a temporary script file in the shuttle folder, runs
// WinSCP on it and removes it. The script holds the password and is never
// logged. The exit code of WinSCP is the success signal: with
// "option batch abort" it is non-zero if any command (e.g. put) fails.
func runWinscp(lines []string, secrets ...string) (string, error) {
	winscp, err := winscpPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(LogPath)
	f, err := CreateTempCompat(dir, "winscp-")
	if err != nil {
		return "", fmt.Errorf("cannot create WinSCP script: %v", err)
	}
	name := f.Name()
	defer os.Remove(name)
	_, werr := f.WriteString(strings.Join(lines, "\r\n") + "\r\n")
	cerr := f.Close()
	if werr != nil {
		return "", fmt.Errorf("cannot write WinSCP script: %v", werr)
	}
	if cerr != nil {
		return "", fmt.Errorf("cannot write WinSCP script: %v", cerr)
	}

	cmd := exec.Command(winscp, "/script="+filepath.Base(name))
	cmd.Dir = filepath.Dir(name)
	out, err := cmd.CombinedOutput()
	output := redactSecrets(string(out), secrets...)
	if err != nil {
		tail := output
		if len(tail) > maxWinscpOutputTail {
			tail = tail[len(tail)-maxWinscpOutputTail:]
		}
		return output, fmt.Errorf("winscp failed: %v; output tail: %s", err, tail)
	}
	return output, nil
}

// TransferManagerSftp sends files via SFTP by driving WinSCP (Windows XP build).
type TransferManagerSftp struct {
	args *Args
	DestCredentials
}

// doWork runs in a endless loop. It reacts on the channel done_files.
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m *TransferManagerSftp) doWork(quit chan int) {
	doWorkImplementation(quit, m, m.srcDir, m.args.sendType, m.args.duration)
}

func (m *TransferManagerSftp) session() winscpSession {
	hostKey := ""
	if m.args != nil {
		hostKey = m.args.hostKey
	}
	if m.args != nil && m.args.insecure {
		hostKey = "*"
		hostKeyInsecureOnce.Do(func() {
			ErrorLogger.Println("SSH host key verification is DISABLED (-insecure)")
		})
	} else if hostKey == "" {
		hostKeyWarnOnce.Do(func() {
			InfoLogger.Println("no -hostkey given; WinSCP must already know the server's host key (accept it once in WinSCP), otherwise every upload fails")
		})
	}
	return winscpSession{User: m.user, Pass: m.pass, Host: m.dst.Host, HostKey: hostKey}
}

// connect_to_server runs a check script, so a missing WinSCP or a bad host key
// fails early.
func (m *TransferManagerSftp) connect_to_server() error {
	_, err := runWinscp(winscpCheckScript(m.session()), m.pass)
	return err
}

// send_file uploads localPath to <dst path>/<remoteRel> in one WinSCP session.
func (m *TransferManagerSftp) send_file(localPath, remoteRel string) (bool, error) {
	remote := path.Join(m.dst.Path, remoteRel)
	if !strings.HasPrefix(remote, "/") {
		remote = "/" + remote
	}
	InfoLogger.Println("Sending...", remote)

	_, err := runWinscp(winscpUploadScript(m.session(), localPath, remote), m.pass)
	return err == nil, err
}
