package main

import (
	"os"
	"path/filepath"
	"time"
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
		case <-time.After(m.retryInterval()):
			m.retryPendingHandOver()
		}
	}
}

// retryInterval is how often pending hand-overs are retried while no new
// item arrives: -duration, but at least one second.
func (m *PrepareManager) retryInterval() time.Duration {
	if m.args.duration < time.Second {
		return time.Second
	}
	return m.args.duration
}

// retryPendingHandOver moves entries that are still waiting in IncomingPath
// into TempPath. A hand-over is blocked while the transfer manager still
// works on an older entry with the same name; without this retry it would
// only be retried when the next item arrives, which may never happen.
func (m *PrepareManager) retryPendingHandOver() {
	pending, err := ReadDirCompat(IncomingPath)
	if err != nil || len(pending) == 0 {
		return
	}
	if err := CopyTempDirectory(); err != nil {
		ErrorLogger.Println(err)
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
	if m.args.preconvert {
		if cerr := RunConverterScripts(tempPath); cerr != nil {
			// Skip the conversion steps only; the raw data is still handed over.
			ErrorLogger.Println("conversion skipped:", cerr)
		} else {
			filePath := tempPath + ".zip"
			fileName := filepath.Base(filePath)
			if err := AddLinkToZip(filepath.Base(filePath), filePath); err != nil {
				ErrorLogger.Println(err)
			}
			if err := os.Rename(filePath, filepath.Join(TempConvertedPath, fileName)); err != nil {
				ErrorLogger.Println(err)
			}
		}
	}
	// On error the entries stay in PreTempPath/IncomingPath for the periodic retry.
	if err = CopyTempDirectory(); err != nil {
		ErrorLogger.Println("hand-over failed, will retry:", err)
		return err
	}
	return nil
}

func newPrepareManager(args *Args, done_files chan string) PrepareManager {
	return PrepareManager{args: args, done_files: done_files}
}
