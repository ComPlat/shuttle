package main

import (
	"net/url"
	"os"
	"path/filepath"
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

	// send_file sends a file via WebDAV
	send_file(path_to_file string, file os.FileInfo) (bool, error)
}

func doWorkImplementation(quit chan int, m TransferManager, srcDir string, sendType string, duration time.Duration) {
	for {
		select {
		case <-quit:
			return
		default:
			items, _ := ReadDirCompat(srcDir)
			for _, file := range items {
				var gErr error = nil
				var ok bool
				to_send := filepath.Join(srcDir, file.Name())

				if !file.IsDir() {
					ok, gErr = m.send_file(to_send, file)
				} else if sendType == "zip" {
					zip_paht, err := zipFolder(to_send)
					gErr = err
					if err == nil {
						if file, err := os.Stat(zip_paht); err != nil {
							gErr = err
						} else {
							ok, gErr = m.send_file(zip_paht, file)
						}
					}

				} else if sendType == "tar" {
					zip_paht, err := tarFolder(to_send)
					gErr = err
					if err == nil {
						if file, err := os.Stat(zip_paht); err != nil {
							gErr = err
						} else {
							ok, gErr = m.send_file(zip_paht, file)
						}
					}

				} else {
					hasChanged := true
					for hasChanged {
						hasChanged = false
						time.Sleep(10 * time.Millisecond)
						gErr = filepath.Walk(to_send, func(path_to_send string, info os.FileInfo, err error) error {
							if err == nil && !info.IsDir() {
								hasChanged = true
								ok, err = m.send_file(path_to_send, info)
								if ok {
									err = os.Remove(path_to_send)
								}
							}

							return err

						})
					}
				}

				if ok {
					err := os.RemoveAll(to_send)
					if err != nil {
						ErrorLogger.Println(err)
					}
				} else if gErr != nil {
					ErrorLogger.Println(gErr)
				}
				time.Sleep(duration / 2)
			}
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
