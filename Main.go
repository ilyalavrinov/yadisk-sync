package main

import log "github.com/sirupsen/logrus"
import "flag"
import "os"
import "time"
import "io/ioutil"
import "path"
import "fmt"
import "syscall"
import "sync"
import "runtime"
import "golang.org/x/crypto/ssh/terminal"
import "github.com/studio-b12/gowebdav"

const (
	argFrom    = "from"
	argTo      = "to"
	argHost    = "host"
	argThreads = "threads"
	argUser    = "user"
	argProfile = "pprof"

	argVerbose      = "verbose"
	argVerboseShort = "v"
	argVerboseDesc  = "Verbose mode - prints extended logs"

	argQuiet      = "quiet"
	argQuietShort = "q"
	argQuietDesc  = "Quiet mode - only problems are reported"

	argUpload   = "upload"
	argDownload = "download"
)

var fromPath = flag.String(argFrom, "", "File or directory used a source of sync")
var toPath = flag.String(argTo, "", "Directory where everything will be stored")
var host = flag.String(argHost, "https://webdav.yandex.ru", "WedDAV server hostname")
var threadsNum = flag.Int(argThreads, runtime.GOMAXPROCS(0)*3, "Number of threads used for transferring")
var user = flag.String(argUser, "", "Username used for authentication")
var profilingEnabled = flag.Bool(argProfile, false, "Enables profiling")
var verbose = flag.Bool(argVerbose, false, argVerboseDesc)
var quiet = flag.Bool(argQuiet, false, argQuietDesc)
var opUpload = flag.Bool(argUpload, false, "Files will be uploaded")
var opDownload = flag.Bool(argDownload, false, "Files will be downloaded")

func init() {
	// initialization of short version of flags
	flag.BoolVar(verbose, argVerboseShort, false, argVerboseDesc)
	flag.BoolVar(quiet, argQuietShort, false, argQuietDesc)
}

func main() {
	flag.Parse()
	if *verbose && *quiet {
		log.Error("Verbose and quiet mode are set simultaneously - please set only one of them")
		os.Exit(1)
	}
	if *verbose {
		log.SetLevel(log.DebugLevel)
	} else if *quiet {
		log.SetLevel(log.WarnLevel)
	}

	log.WithFields(log.Fields{"arg": argFrom, "value": *fromPath}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argTo, "value": *toPath}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argThreads, "value": *threadsNum}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argUser, "value": *user}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argHost, "value": *host}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argProfile, "value": *profilingEnabled}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argVerbose, "value": *verbose}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argQuiet, "value": *quiet}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argUpload, "value": *opUpload}).Debug("Argument")
	log.WithFields(log.Fields{"arg": argDownload, "value": *opDownload}).Debug("Argument")

	isCorrectArgs := true
	if fromPath == nil || *fromPath == "" {
		log.WithFields(log.Fields{"arg": argFrom}).Error("Mandatory argument missing")
		isCorrectArgs = false
	}

	if *opUpload == *opDownload {
		log.WithFields(log.Fields{"upload": *opUpload, "download": *opDownload}).Error("Incorrect argument setting")
		isCorrectArgs = false
	}

	if isCorrectArgs == false {
		log.Error("Incorrect argument usage, printing help")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *profilingEnabled {
		startProfiling()
		defer stopProfiling()
	}

	if *user == "" {
		*user = requestFromStdin("user")
		log.WithFields(log.Fields{"user": *user}).Debug("User has been requested from stdin")
	}

	pw := requestFromStdin("password")
	opts := TransferSettings{Host: *host,
		User:     *user,
		Password: pw}

	var tasks []TransferTask
	if *opUpload {
		tasks = createUploadList(*fromPath, *toPath)
	} else {
		tasks = createDownloadList(*fromPath, *toPath, opts)
	}

	log.WithFields(log.Fields{"count": len(tasks)}).Info("List of transfers is ready")
	tasksCh := make(chan TransferTask, *threadsNum*100)
	resultsCh := make(chan TransferResult, *threadsNum)
	for i := 0; i < *threadsNum; i++ {
		worker := NewWorker(opts, tasksCh, resultsCh)
		worker.Run()
	}

	wg := sync.WaitGroup{}
	summary := NewTransferSummary()
	go collectResults(resultsCh, &wg, len(tasks), summary)
	wg.Add(1)

	t1 := time.Now()

	for _, t := range tasks {
		tasksCh <- t
	}

	wg.Wait()
	t2 := time.Now()
	summary.clockTimeSpent = t2.Sub(t1)

	summary.print()
}

func createUploadList(fpath, uploadDir string) []TransferTask {
	result := make([]TransferTask, 0, 1)
	log.WithFields(log.Fields{"local": fpath, "remote": uploadDir}).Debug("Creating upload list")
	stat, err := os.Stat(fpath)
	if err != nil {
		panic(err)
	}

	if stat.Mode().IsRegular() {
		result = append(result, TransferTask{
			Operation: OperationUpload,
			From:      fpath,
			To:        path.Join(uploadDir, stat.Name())})
	} else if stat.Mode().IsDir() {
		content, err := ioutil.ReadDir(fpath)
		if err != nil {
			panic(err)
		}
		for _, info := range content {
			result = append(result, createUploadList(
				path.Join(fpath, info.Name()),
				path.Join(uploadDir, stat.Name()))...)
		}
	} else {
		panic("Unhandled path mode")
	}
	return result
}

func createDownloadList(remotePath, dlDir string, connOpts TransferSettings) []TransferTask {
	client := gowebdav.NewClient(connOpts.Host, connOpts.User, connOpts.Password)
	return _createDownloadList(remotePath, dlDir, client)

}

func _createDownloadList(remotePath, dlDir string, client *gowebdav.Client) []TransferTask {
	result := make([]TransferTask, 0, 1)
	log.WithFields(log.Fields{"local": dlDir, "remote": remotePath}).Debug("Creating download list")

	fstat, err := client.Stat(remotePath)
	if err != nil {
		log.Panic("Cannot get remote path stats", err)
	}

	if fstat.IsDir() {
		dirContents, err := client.ReadDir(remotePath)
		if err != nil {
			log.Panic("Could not read remote directory contents", err)
		}
		for _, finfo := range dirContents {
			result = append(result, _createDownloadList(
				path.Join(remotePath, finfo.Name()),
				path.Join(dlDir, fstat.Name()),
				client)...)
		}
	} else {
		result = append(result, TransferTask{
			Operation: OperationDownload,
			From:      remotePath,
			To:        path.Join(dlDir, fstat.Name())})
	}
	return result
}

func requestFromStdin(what string) string {
	fmt.Printf("Enter %s: ", what)
	byteData, _ := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println("")
	return string(byteData)
}

// TransferSummary provides accumulated statistics about how did the transferring go
type TransferSummary struct {
	statuses             map[TransferStatus]int
	totalSizeTransferred int64
	totalTimeSpent       time.Duration
	clockTimeSpent       time.Duration

	failedToTransfer []string
}

// NewTransferSummary initializes new TransferSummary
func NewTransferSummary() *TransferSummary {
	s := &TransferSummary{statuses: make(map[TransferStatus]int, StatusLast),
		failedToTransfer: make([]string, 0)}
	return s
}

func (s TransferSummary) print() {
	log.WithFields(log.Fields{
		"done":    s.statuses[StatusDone],
		"skipped": s.statuses[StatusAlreadyExist],
		"failed":  s.statuses[StatusFailed]}).Info("Totals")

	log.WithFields(log.Fields{
		"B":  s.totalSizeTransferred,
		"KB": s.totalSizeTransferred / 1024,
		"MB": s.totalSizeTransferred / 1024 / 1024,
		"GB": s.totalSizeTransferred / 1024 / 1024 / 1024}).Info("Total processed size")

	log.WithFields(log.Fields{
		"spent":   s.totalTimeSpent,
		"bytes/s": float64(s.totalSizeTransferred) / s.totalTimeSpent.Seconds()}).Info("Raw processing stats (as if in 1 thread)")

	log.WithFields(log.Fields{
		"spent":   s.totalTimeSpent,
		"bytes/s": float64(s.totalSizeTransferred) / s.clockTimeSpent.Seconds()}).Info("Actual processing stats")

	failedN := len(s.failedToTransfer)
	if failedN > 0 {
		log.WithField("failed", failedN).Warn("Failed transfers")
		fname := fmt.Sprintf("transfer_failed_%s.list", time.Now().Format("20060102150405"))
		f, err := os.Create(fname)
		defer f.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"file": fname,
				"list": s.failedToTransfer}).Warn("Failed to create a file for failed transfers")
		} else {
			for _, failedTransfer := range s.failedToTransfer {
				f.WriteString(failedTransfer)
				f.WriteString("\n")
			}
			log.WithFields(log.Fields{"file": fname}).Warn("Failed transfers have beed written")
		}
	}
}

func collectResults(results <-chan TransferResult, wg *sync.WaitGroup, resultsExpected int, summary *TransferSummary) {
	for res := range results {
		summary.statuses[res.Status]++
		resultsExpected--
		logger := log.WithFields(log.Fields{
			"from":      res.Task.From,
			"remaining": resultsExpected})
		switch res.Status {
		case StatusDone:
			logger.WithFields(log.Fields{
				"spent":   res.TimeSpent,
				"size":    res.Size,
				"bytes/s": float64(res.Size) / res.TimeSpent.Seconds()}).Info("Transferred")
			summary.totalSizeTransferred += res.Size
			summary.totalTimeSpent += res.TimeSpent
		case StatusAlreadyExist:
			logger.Debug("Already exists, skipping transfer")
		case StatusFailed:
			logger.WithFields(log.Fields{"to": res.Task.To, "error": res.Error}).Error("Transfer failed")
			summary.failedToTransfer = append(summary.failedToTransfer, res.Task.From)
		default:
			panic("Unhandled status")
		}

		if resultsExpected == 0 {
			break
		}
	}
	wg.Done()
}
