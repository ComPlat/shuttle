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
)

var tempCounter = 0

func winscp(cmd ...string) error {
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}

	tempCounter = (tempCounter + 1) % 10000
	tempFilename := fmt.Sprintf("temp_%d.bat", tempCounter)

	// Next we'll look at a slightly more involved case
	// where we pipe data to the external process on its
	// `stdin` and collect the results from its `stdout`.
	f, err := os.Create(tempFilename)
	_, err = f.WriteString(filepath.Dir(ex) + "\\winscp.com " + strings.Join(cmd, " "))
	_ = f.Close()
	defer os.Remove(tempFilename)
	grepCmd := exec.Command(tempFilename)

	// Here we explicitly grab input/output pipes, start
	// the process, write some input to it, read the
	// resulting output, and finally wait for the process
	// to exit.
	_, err = grepCmd.CombinedOutput()
	grepCmd.Wait()
	return err
}

// TransferManagerSftp reacts on the channel done_files.
// If folder of file is ready to send it sends it via WebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set.
type TransferManagerSftp struct {
	args *Args
	DestCredentials
}

// doWork runs in a endless loop. It reacts on the channel done_files.
// If folder of file is ready to send it sends it via HWebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m *TransferManagerSftp) doWork(quit chan int) {
	doWorkImplementation(quit, m, m.srcDir, m.args.sendType, m.args.duration)
}

func (m *TransferManagerSftp) connect_to_server() error {
	//user := m.args.user
	//password := m.args.pass

	sftpConnStr := fmt.Sprintf("sftp://%s:%s@%s/", m.user, m.pass, m.dst.Host)

	//return winscp("sftp://martin:Kokoa10!@192.168.56.108:22/home/martin/data_sft_test")
	return winscp(sftpConnStr)
}

// send_file uploads localPath to <dst path>/<remoteRel>.
func (m *TransferManagerSftp) send_file(localPath, remoteRel string) (bool, error) {
	remote := path.Join(m.dst.Path, remoteRel)
	InfoLogger.Println("Sending...", remote)

	sftpConnStr := fmt.Sprintf("sftp://%s:%s@%s/", m.user, m.pass, m.dst.Host)
	if dir := path.Dir(remoteRel); dir != "." {
		for _, s := range strings.Split(dir, "/") {
			cwp := m.dst.Path + "/" + s
			_ = winscp("/command \"open "+sftpConnStr+"\"", fmt.Sprintf("\"mkdir \"\"%s\"\"\"", cwp), "\"exit\"")
		}
	}

	err := winscp("/command", "\"open "+sftpConnStr+"\"",
		fmt.Sprintf("\"put \"\"%s\"\" \"\"%s\"\"\"", localPath, remote),
		"\"exit\"")

	return err == nil, err
}
