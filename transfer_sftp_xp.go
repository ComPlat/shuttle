//go:build !go1.11
// +build !go1.11

package main

import (
	"fmt"
	"os"
	"os/exec"
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
}

// doWork runs in a endless loop. It reacts on the channel done_files.
// If folder of file is ready to send it sends it via HWebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m *TransferManagerSftp) doWork(quit chan int) {
	doWorkImplementation(quit, m, m.args)
}

func (m *TransferManagerSftp) connect_to_server() error {
	//user := m.args.user
	//password := m.args.pass

	sftpConnStr := fmt.Sprintf("sftp://%s:%s@%s/", m.args.user, m.args.pass, m.args.dst.Host)

	//return winscp("sftp://martin:Kokoa10!@192.168.56.108:22/home/martin/data_sft_test")
	return winscp(sftpConnStr)
}

// send_file sends a file via WebDAV
func (m *TransferManagerSftp) send_file(path_to_file string, file os.FileInfo) (bool, error) {
	var webdavFilePath, urlPathDir string

	if m.args.sendType == "file" {
		webdavFilePath = file.Name()
	} else if relpath, err := filepath.Rel(TempPath, path_to_file); err == nil {
		webdavFilePath = strings.Replace(relpath, string(os.PathSeparator), "/", -1)
		webdavFilePath = strings.TrimPrefix(webdavFilePath, "./")
	} else {
		return false, err
	}
	webdavFilePath = filepath.Join(m.args.dst.Path, webdavFilePath)
	urlPathDir = filepath.Dir(webdavFilePath)
	InfoLogger.Println("Sending...", webdavFilePath)

	sftpConnStr := fmt.Sprintf("sftp://%s:%s@%s/", m.args.user, m.args.pass, m.args.dst.Host)
	if relpath, err := filepath.Rel(m.args.dst.Path, urlPathDir); err == nil {
		urlPathDir = strings.Replace(relpath, string(os.PathSeparator), "/", -1)
	} else {
		return false, err
	}

	cwp := m.args.dst.Path
	webdavFilePath = strings.Replace(webdavFilePath, string(os.PathSeparator), "/", -1)
	for _, s := range strings.Split(urlPathDir, "/") {
		cwp := cwp + "/" + s
		_ = winscp("/command \"open "+sftpConnStr+"\"", fmt.Sprintf("\"mkdir \"\"%s\"\"\"", cwp), "\"exit\"")
	}

	err := winscp("/command", "\"open "+sftpConnStr+"\"",
		fmt.Sprintf("\"put \"\"%s\"\" \"\"%s\"\"\"", path_to_file, webdavFilePath),
		"\"exit\"")

	return err == nil, err
}
