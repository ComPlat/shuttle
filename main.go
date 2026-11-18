package main

import (
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"
)

var (
	InfoLogger        *log.Logger
	ErrorLogger       *log.Logger
	args              Args
	tr                *http.Transport = nil
	LogPath           string
	TempPath          string
	TempConvertedPath string
	PreTempPath       string
	FlatTarTempPath   string
	PreScriptPath     string
	// IncomingPath is the staging folder for the hand-over into TempPath:
	// entries are copied here first and then renamed into TempPath, so the
	// transfer managers never see a half-copied file (WP 3.4).
	IncomingPath string
	// RegisterPath is the JSON file of the sent register (WP 3.9).
	RegisterPath string
	// KnownHostsPath stores SSH host keys trusted on first use (WP 6.2),
	// in OpenSSH known_hosts format.
	KnownHostsPath string
	// ArchivePath holds zip/tar archives built by the transfer managers,
	// outside the folder they scan (WP 3.2).
	ArchivePath string
	// FlatTarOutPath holds finished flat_tar archives, outside
	// FlatTarTempPath, so they are never archived again (WP 3.7).
	FlatTarOutPath string
)

// instanceDirName returns the instance name as used in the folder name
// shuttle_<name>. On Windows the name is cleaned with safeDirName, because
// characters such as : or a trailing dot make MkdirAll fail. Elsewhere it is
// returned unchanged: changing it would move an existing install to a new
// folder with an empty sent register and cause a full re-upload.
func instanceDirName(name string) string {
	if runtime.GOOS == "windows" {
		return safeDirName(name)
	}
	return name
}

// setupPaths sets every working-folder global for home directory home and
// instance name name. It creates no folders.
func setupPaths(home, name string) {
	mainShuttleFolderName := filepath.Join(home, "shuttle")
	shuttleFolderName := filepath.Join(mainShuttleFolderName, "shuttle_"+instanceDirName(name))
	TempPath = filepath.Join(shuttleFolderName, "shuttle_temp")
	TempConvertedPath = filepath.Join(shuttleFolderName, "shuttle_converted_temp")
	PreTempPath = filepath.Join(shuttleFolderName, "shuttle_pre_temp")
	FlatTarTempPath = filepath.Join(shuttleFolderName, "shuttle_flat_tar_temp")
	IncomingPath = filepath.Join(shuttleFolderName, "shuttle_incoming")
	ArchivePath = filepath.Join(shuttleFolderName, "shuttle_archive_temp")
	FlatTarOutPath = filepath.Join(shuttleFolderName, "shuttle_flat_tar_out")
	RegisterPath = filepath.Join(shuttleFolderName, "sent_register.json")
	KnownHostsPath = filepath.Join(shuttleFolderName, "known_hosts")
	PreScriptPath = filepath.Join(mainShuttleFolderName, "scripts")
	LogPath = filepath.Join(shuttleFolderName, "shuttle_log.txt")
}

// init initializes the logger and parses CMD args.
func initTool() {
	args = GetCmdArgs()

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	setupPaths(usr.HomeDir, args.name)

	newPaths := []string{TempPath, PreTempPath, PreScriptPath, IncomingPath, ArchivePath}
	if args.sendType == "flat_tar" {
		newPaths = append(newPaths, FlatTarTempPath, FlatTarOutPath)
	}
	if args.preconvert {
		newPaths = append(newPaths, TempConvertedPath)
	}
	for _, newPath := range newPaths {
		if err := os.MkdirAll(newPath, os.ModePerm); err != nil {
			ErrorLogger.Println(err)
			panic("")
		}
	}

	logFile, err := os.OpenFile(LogPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		panic(err)
	}
	mw := io.MultiWriter(os.Stdout, logFile)

	InfoLogger = log.New(mw, "-> INFO: ", log.Ldate|log.Ltime)
	ErrorLogger = log.New(mw, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	// Read in the cert file
}

func initArgs() {
	cfg, warnings := buildTLSConfig(unsetPlaceholder(args.crt), args.insecure, legacyTLSRoots, x509.SystemCertPool)
	for _, w := range warnings {
		ErrorLogger.Println("WARNING: " + w)
	}
	tr = &http.Transport{TLSClientConfig: cfg}
}

// main starts the ELN file watcher. See README for more information.
func main() {
	initTool()
	initArgs()
	now := time.Now()
	InfoLogger.Println("Starting at ", now.Format(time.RFC822))
	defer (func() {
		now := time.Now()
		InfoLogger.Println("Done at ", now.Format(time.RFC822))
	})()

	// Chain as communication channel between the file watcher and the transfer manager
	done_files := make(chan string, 20)
	// For potential (not jet implemented quit conditions)
	quit := make(chan int)
	InfoLogger.Print(args.LogString())
	register := LoadSentRegister(RegisterPath)
	if register.Len() == 0 {
		InfoLogger.Println("Sent register: 0 entries loaded, empty register: all stable files will be sent once")
	} else {
		InfoLogger.Printf("Sent register: %d entries loaded from %s", register.Len(), RegisterPath)
	}
	pm := newProcessManager(&args, done_files, register)
	go pm.doWork(quit)

	prm := newPrepareManager(&args, done_files)
	go prm.doWork(quit)

	tm := newFileTransferManager(&args)
	if err := tm.connect_to_server(); err != nil {
		ErrorLogger.Println("Error connecting: ", err)
		log.Fatal(err)
	}
	go tm.doWork(quit)

	if args.preconvert {
		tmc := newConvertedTransferManager(&args)
		if err := tmc.connect_to_server(); err != nil {
			ErrorLogger.Println("Error connecting: ", err)
			log.Fatal(err)
		}
		go tmc.doWork(quit)
	}

	for {
		time.Sleep(args.duration * 20)
	}
}
