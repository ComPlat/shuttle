package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"path"
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
	// ArchivePath holds zip/tar archives built by the transfer managers,
	// outside the folder they scan (WP 3.2).
	ArchivePath string
	// FlatTarOutPath holds finished flat_tar archives, outside
	// FlatTarTempPath, so they are never archived again (WP 3.7).
	FlatTarOutPath string
)

// init initializes the logger and parses CMD args.
func initTool() {
	args = GetCmdArgs()

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	wd := usr.HomeDir
	shuttleFolderName := fmt.Sprintf("shuttle_%s", args.name)
	mainShuttleFolderName := path.Join(wd, "shuttle")
	shuttleFolderName = path.Join(mainShuttleFolderName, shuttleFolderName)
	TempPath = path.Join(shuttleFolderName, "shuttle_temp")
	TempConvertedPath = path.Join(shuttleFolderName, "shuttle_converted_temp")
	PreTempPath = path.Join(shuttleFolderName, "shuttle_pre_temp")
	FlatTarTempPath = path.Join(shuttleFolderName, "shuttle_flat_tar_temp")
	IncomingPath = path.Join(shuttleFolderName, "shuttle_incoming")
	ArchivePath = path.Join(shuttleFolderName, "shuttle_archive_temp")
	FlatTarOutPath = path.Join(shuttleFolderName, "shuttle_flat_tar_out")
	RegisterPath = path.Join(shuttleFolderName, "sent_register.json")
	PreScriptPath = path.Join(mainShuttleFolderName, "scripts")

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

	LogPath = path.Join(shuttleFolderName, "shuttle_log.txt")

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
	isCert := len(args.crt) > 0

	// Get the SystemCertPool, continue with an empty pool on error
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if isCert {
		certs, err := ReadFileCompat(args.crt)
		if err == nil {
			if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
				ErrorLogger.Println("No certs appended, using system certs only")
			}
		}
	}

	// Trust the augmented cert pool in our client
	config := &tls.Config{
		InsecureSkipVerify: true,
		RootCAs:            rootCAs,
	}

	tr = &http.Transport{TLSClientConfig: config}

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
