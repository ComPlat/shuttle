package main

import (
	"log"
	"os"
)

// TransferManager reacts on the channel done_files.
// If folder of file is ready to send it sends it via WebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set.
type PrepareManager struct {
	args       *Args
	done_files chan string
}

// doWork runs in a endless loop. It reacts on the channel done_files.
// If folder of file is ready to send it sends it via HWebDAV (HTTP) to <CMD arg -dst>.
// It also initializes the zipping if <CMD arg -zip> is set
// It terminates as soon as a value is pushed into quit. Run in extra goroutine.
func (m *PrepareManager) doWork(quit chan int) {
	InfoLogger.Println("Started transfer process.")

	for {

		select {
		case <-quit:
			InfoLogger.Println("Quit transfer process.")
			return
		case to_send := <-m.done_files:
			_, err := os.Stat(to_send)
			if err != nil {
				ErrorLogger.Println(err)
				break
			}
			tempPath, err := CopyPreTempDirectory(to_send)
			if m.args.sendType == "flat_tar" {
				err := os.Remove(to_send)
				if err != nil {
					ErrorLogger.Println(err)
				}
			}
			if err != nil {
				ErrorLogger.Println(err)
			}

			RunPreScripts(tempPath)
			err = CopyTempDirectory()
			if err != nil {
				log.Fatal(err)
			}

		}
	}
}

func newPrepareManager(args *Args, done_files chan string) PrepareManager {
	return PrepareManager{args: args, done_files: done_files}
}
