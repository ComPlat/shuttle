package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

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

func doWorkImplementation(quit chan int, m TransferManager, args *Args) {
	for {
		select {
		case <-quit:
			return
		default:
			items, _ := ioutil.ReadDir(TempPath)
			for _, file := range items {
				var gErr error = nil
				var ok bool
				to_send := filepath.Join(TempPath, file.Name())

				if !file.IsDir() {
					ok, gErr = m.send_file(to_send, file)
				} else if args.sendType == "zip" {
					zip_paht, err := zipFolder(to_send)
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
				time.Sleep(args.duration / 2)
			}
		}
	}
}

func newTransferManager(args *Args) TransferManager {
	if args.tType == "webdav" {
		return &TransferManagerWebdav{args: args}
	} else if args.tType == "sftp" {
		return &TransferManagerSftp{args: args}
	}

	panic("Transfer type is not implemented")
}
