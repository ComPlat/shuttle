package main

import (
	"log"
	"os"
	"path"
	"path/filepath"
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
			// Errors are logged inside prepareOne; the next item is handled regardless.
			_ = m.prepareOne(to_send)
		}
	}
}

// prepareOne copies one finished item into PreTempPath, runs the scripts
// and hands the result over to TempPath. If the copy into PreTempPath fails,
// the source is left untouched and no script runs.
func (m *PrepareManager) prepareOne(toSend string) error {
	_, err := os.Stat(toSend)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}
	tempPath, err := CopyPreTempDirectory(toSend)
	if err != nil {
		ErrorLogger.Println(err)
		// Do not leave a partial copy behind: the next hand-over would pass it on.
		_ = os.RemoveAll(tempPath)
		return err
	}
	if m.args.sendType == "flat_tar" {
		// The tar was copied completely, so the original is no longer needed.
		if err := os.Remove(toSend); err != nil {
			ErrorLogger.Println(err)
		}
	}

	RunPreScripts(tempPath)
	if args.preconvert {
		RunConverterScripts(tempPath)
		filePath := tempPath + ".zip"
		fileName := filepath.Base(filePath)
		err := AddLinkToZip(filepath.Base(filePath), filePath)
		if err != nil {
			ErrorLogger.Println(err)
		}
		err = os.Rename(filePath, path.Join(TempConvertedPath, fileName))
		if err != nil {
			ErrorLogger.Println(err)
		}
	}
	err = CopyTempDirectory()
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func newPrepareManager(args *Args, done_files chan string) PrepareManager {
	return PrepareManager{args: args, done_files: done_files}
}
