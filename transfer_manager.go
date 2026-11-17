package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DestCredentials struct {
	user, srcDir, pass string
	dst                url.URL
}

// TransferManager reacts on the channel done_files.
// If folder of file is ready to send it sends it via WebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set.
type TransferManager interface {
	// doWork runs in a endless loop. It reacts on the channel done_files.
	// If folder of file is ready to send it sends it via HWebDAV (HTTP) to <CMD arg -dst>.
	// It also initializes the zipping if <CMD arg -zip> is set
	// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
	doWork(quit chan int)

	connect_to_server() error

	// send_file uploads localPath to <destination base>/<remoteRel>.
	// remoteRel is relative and "/"-separated, without a leading "/".
	// It returns true only if the upload is confirmed complete.
	send_file(localPath, remoteRel string) (bool, error)
}

// remoteRelPath returns the "/"-separated path of localPath relative to
// srcDir. It fails if localPath is not inside srcDir.
func remoteRelPath(srcDir, localPath string) (string, error) {
	rel, err := filepath.Rel(srcDir, localPath)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside of %s", localPath, srcDir)
	}
	return rel, nil
}

// backoffDelay returns the wait after failedCycles consecutive failing
// cycles: 0 for none, otherwise 1s * 2^(n-1), capped at max.
func backoffDelay(failedCycles int, max time.Duration) time.Duration {
	if failedCycles <= 0 {
		return 0
	}
	d := time.Second
	for i := 1; i < failedCycles; i++ {
		if d >= max || d > time.Hour {
			break
		}
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

// sendAndRemove sends one local file and removes it after a confirmed upload.
// It returns false if the file is still in place.
func sendAndRemove(m TransferManager, srcDir, localPath string) bool {
	rel, err := remoteRelPath(srcDir, localPath)
	if err != nil {
		ErrorLogger.Println(err)
		return false
	}
	ok, err := m.send_file(localPath, rel)
	if !ok {
		if err == nil {
			err = fmt.Errorf("sending %s was not confirmed", localPath)
		}
		ErrorLogger.Println(err)
		return false
	}
	if err := os.Remove(localPath); err != nil {
		ErrorLogger.Println(err)
	}
	return true
}

// sendArchive builds the archive of folder (zip or tar), moves it into
// ArchivePath, sends it once and always removes it afterwards. The folder is
// removed only after a successful send.
func sendArchive(m TransferManager, folder, sendType string) bool {
	var archive string
	var err error
	if sendType == "zip" {
		archive, err = zipFolder(folder)
	} else {
		archive, err = tarFolder(folder)
	}
	if err != nil {
		ErrorLogger.Println(err)
		if archive != "" {
			_ = os.Remove(archive)
		}
		return false
	}
	staged := filepath.Join(ArchivePath, filepath.Base(archive))
	if err := os.Rename(archive, staged); err != nil {
		ErrorLogger.Println(err)
		_ = os.Remove(archive)
		return false
	}
	defer func() {
		if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
			ErrorLogger.Println(err)
		}
	}()
	rel, err := remoteRelPath(ArchivePath, staged)
	if err != nil {
		ErrorLogger.Println(err)
		return false
	}
	ok, err := m.send_file(staged, rel)
	if !ok {
		if err == nil {
			err = fmt.Errorf("sending %s was not confirmed", staged)
		}
		ErrorLogger.Println(err)
		return false
	}
	if err := os.RemoveAll(folder); err != nil {
		ErrorLogger.Println(err)
	}
	return true
}

// sendFolderFiles does one pass over folder and sends every regular file.
// Sent files are removed, failed ones stay. It returns the number of failures.
// The folder is removed if no regular file is left in it.
func sendFolderFiles(m TransferManager, srcDir, folder string, itemPause time.Duration) int {
	failed := 0
	err := filepath.Walk(folder, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			ErrorLogger.Println(err)
			failed++
			return nil
		}
		if info.Mode().IsRegular() {
			if !sendAndRemove(m, srcDir, p) {
				failed++
			}
		}
		return nil
	})
	if err != nil {
		ErrorLogger.Println(err)
		failed++
	}
	hasFiles := false
	_ = filepath.Walk(folder, func(p string, info os.FileInfo, err error) error {
		if err != nil || (info != nil && !info.IsDir()) {
			hasFiles = true
		}
		return nil
	})
	if !hasFiles {
		if err := os.RemoveAll(folder); err != nil {
			ErrorLogger.Println(err)
		}
	}
	return failed
}

// transferCycle sends everything currently in srcDir once.
// It returns the number of failed sends.
func transferCycle(m TransferManager, srcDir, sendType string, itemPause time.Duration) int {
	items, err := ReadDirCompat(srcDir)
	if err != nil {
		ErrorLogger.Println(err)
		return 1
	}
	failed := 0
	for _, file := range items {
		toSend := filepath.Join(srcDir, file.Name())
		if !file.IsDir() {
			if !sendAndRemove(m, srcDir, toSend) {
				failed++
			}
		} else if sendType == "zip" || sendType == "tar" {
			if !sendArchive(m, toSend, sendType) {
				failed++
			}
		} else {
			failed += sendFolderFiles(m, srcDir, toSend, itemPause)
		}
		time.Sleep(itemPause)
	}
	return failed
}

func doWorkImplementation(quit chan int, m TransferManager, srcDir string, sendType string, duration time.Duration) {
	failedCycles := 0
	for {
		select {
		case <-quit:
			return
		default:
		}
		failed := transferCycle(m, srcDir, sendType, duration/2)
		wait := duration / 2
		if failed > 0 {
			failedCycles++
			wait = backoffDelay(failedCycles, duration)
			InfoLogger.Printf("transfer failed %d cycle(s) in a row, waiting %s\n", failedCycles, wait)
		} else {
			failedCycles = 0
		}
		select {
		case <-quit:
			return
		case <-time.After(wait):
		}
	}
}

func newFileTransferManager(args *Args) TransferManager {
	dest := DestCredentials{
		user:   args.user,
		pass:   args.pass,
		dst:    args.dst,
		srcDir: TempPath,
	}

	return newTransferManager(args, &dest)
}

func newConvertedTransferManager(args *Args) TransferManager {
	dest := DestCredentials{
		user:   args.userConverter,
		pass:   args.passConverter,
		dst:    args.dstConverter,
		srcDir: TempConvertedPath,
	}

	return newTransferManager(args, &dest)
}

func newTransferManager(args *Args, dest *DestCredentials) TransferManager {
	if args.tType == "webdav" {
		return &TransferManagerWebdav{DestCredentials: *dest, args: args}
	} else if args.tType == "sftp" {
		return &TransferManagerSftp{DestCredentials: *dest, args: args}
	}

	panic("Transfer type is not implemented")
}
