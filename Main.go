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
)

var fromPath = flag.String(argFrom, "", "File or directory which could be uploaded")
var toPath = flag.String(argTo, "", "Remote directory where everything will be uploaded")
var host = flag.String(argHost, "https://webdav.yandex.ru", "WedDAV server hostname")
var threadsNum = flag.Int(argThreads, runtime.GOMAXPROCS(0)*3, "Number of threads used for uploading")
var user = flag.String(argUser, "", "Username used for authentication")
var profilingEnabled = flag.Bool(argProfile, false, "Enables profiling")
var verbose = flag.Bool(argVerbose, false, argVerboseDesc)
var quiet = flag.Bool(argQuiet, false, argQuietDesc)

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

	isCorrectArgs := true
	if fromPath == nil || *fromPath == "" {
		log.WithFields(log.Fields{"arg": argFrom}).Error("Mandatory argument missing")
		isCorrectArgs = false
	}

	if isCorrectArgs == false {
		log.Error("Mandatory argument missing, printing help")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *profilingEnabled {
		startProfiling()
		defer stopProfiling()
	}

	uploads := createUploadList(*fromPath, *toPath)
	log.WithFields(log.Fields{"count": len(uploads)}).Info("List of uploads is ready")

	if *user == "" {
		*user = requestFromStdin("user")
		log.WithFields(log.Fields{"user": *user}).Debug("User has been requested from stdin")
	}

	pw := requestFromStdin("password")
	opts := UploadOptions{Host: *host,
		User:     *user,
		Password: pw}

	uploadTasks := make(chan UploadTask, *threadsNum*100)
	resultsCh := make(chan UploadResult, *threadsNum)
	for i := 0; i < *threadsNum; i++ {
		uploader := NewUploader(opts, uploadTasks, resultsCh)
		uploader.Run()
	}

	wg := sync.WaitGroup{}
	summary := NewUploadSummary()
	go collectResults(resultsCh, &wg, len(uploads), summary)
	wg.Add(1)

	t1 := time.Now()

	for _, u := range uploads {
		uploadTasks <- u
	}

	wg.Wait()
	t2 := time.Now()
	summary.clockTimeSpent = t2.Sub(t1)

	summary.print()
}

func createUploadList(fpath, uploadDir string) []UploadTask {
	result := make([]UploadTask, 0, 1)
	// log.Printf("Creating upload list for: %s (with uploadDir %s)", fpath, uploadDir)
	stat, err := os.Stat(fpath)
	if err != nil {
		panic(err)
	}

	if stat.Mode().IsRegular() {
		result = append(result, UploadTask{From: fpath,
			To: path.Join(uploadDir, stat.Name())})
	} else if stat.Mode().IsDir() {
		content, err := ioutil.ReadDir(fpath)
		if err != nil {
			panic(err)
		}
		for _, info := range content {
			result = append(result, createUploadList(path.Join(fpath, info.Name()),
				path.Join(uploadDir, stat.Name()))...)
		}
	} else {
		panic("Unhandled path mode")
	}
	return result
}

func requestFromStdin(what string) string {
	fmt.Printf("Enter %s: ", what)
	byteData, _ := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println("")
	return string(byteData)
}

// UploadSummary provides accumulated statistics about how did the uploading go
type UploadSummary struct {
	statuses          map[UploadStatus]int
	totalSizeUploaded int64
	totalTimeSpent    time.Duration
	clockTimeSpent    time.Duration

	failedToUpload []string
}

// NewUploadSummary initializes new UploadSummary
func NewUploadSummary() *UploadSummary {
	s := &UploadSummary{statuses: make(map[UploadStatus]int, StatusLast),
		failedToUpload: make([]string, 0)}
	return s
}

func (s UploadSummary) print() {
	log.WithFields(log.Fields{
		"uploaded": s.statuses[StatusUploaded],
		"skipped":  s.statuses[StatusAlreadyExist],
		"failed":   s.statuses[StatusFailed]}).Info("Totals")
	//fmt.Printf("Total uploaded: %d; skipped: %d; failed: %d\n", s.statuses[StatusUploaded], s.statuses[StatusAlreadyExist], s.statuses[StatusFailed])

	log.WithFields(log.Fields{
		"B":  s.totalSizeUploaded,
		"KB": s.totalSizeUploaded / 1024,
		"MB": s.totalSizeUploaded / 1024 / 1024,
		"GB": s.totalSizeUploaded / 1024 / 1024 / 1024}).Info("Total processed size")
	//fmt.Printf("Total uploaded bytes: %d\n", s.totalSizeUploaded)

	log.WithFields(log.Fields{
		"spent":   s.totalTimeSpent,
		"bytes/s": float64(s.totalSizeUploaded) / s.totalTimeSpent.Seconds()}).Info("Raw processing stats (as if in 1 thread)")
	//fmt.Printf("Raw time spent for upload: %s\n", s.totalTimeSpent)
	//fmt.Printf("Raw average speed: %f bytes/s\n", float64(s.totalSizeUploaded) / s.totalTimeSpent.Seconds())

	log.WithFields(log.Fields{
		"spent":   s.totalTimeSpent,
		"bytes/s": float64(s.totalSizeUploaded) / s.clockTimeSpent.Seconds()}).Info("Actual processing stats")
	//fmt.Printf("Clock time spent for upload: %s\n", s.clockTimeSpent)
	//fmt.Printf("Average speed: %f bytes/s\n", float64(s.totalSizeUploaded) / s.clockTimeSpent.Seconds())

	failedN := len(s.failedToUpload)
	//fmt.Printf("Failed uploads: %d\n", failedN)
	if failedN > 0 {
		log.WithField("failed", failedN).Warn("Failed transactions")
		fname := fmt.Sprintf("upload_failed_%s.list", time.Now().Format("20060102150405"))
		f, err := os.Create(fname)
		defer f.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"file": fname,
				"list": s.failedToUpload}).Warn("Failed to create a file for failed uploads")
			//fmt.Printf("Failed to create file %s for failed uploads. Failed upload list: %+v", fname, s.failedToUpload)
		} else {
			for _, failedUpload := range s.failedToUpload {
				f.WriteString(failedUpload)
				f.WriteString("\n")
			}
			log.WithFields(log.Fields{"file": fname}).Warn("Failed uploads have beed written")
			//fmt.Printf("Failed uploads have beed written to file %s", fname)
		}
	}
}

func collectResults(results <-chan UploadResult, wg *sync.WaitGroup, resultsExpected int, summary *UploadSummary) {
	for res := range results {
		summary.statuses[res.Status]++
		switch res.Status {
		case StatusUploaded:
			log.WithFields(log.Fields{
				"from":    res.Task.From,
				"spent":   res.TimeSpent,
				"size":    res.Size,
				"bytes/s": float64(res.Size) / res.TimeSpent.Seconds()}).Info("Uploaded")
			summary.totalSizeUploaded += res.Size
			summary.totalTimeSpent += res.TimeSpent
		case StatusAlreadyExist:
			log.WithFields(log.Fields{
				"from": res.Task.From}).Debug("Already exists, skipping upload")
		case StatusFailed:
			log.WithFields(log.Fields{"from": res.Task.From, "to": res.Task.To, "error": res.Error}).Error("Upload failed")
			summary.failedToUpload = append(summary.failedToUpload, res.Task.From)
		default:
			panic("Unhandled status")
		}
		resultsExpected--
		if resultsExpected == 0 {
			break
		}
	}
	wg.Done()
}
