package main

import "log"
import "flag"
import "os"
import "time"
import "io/ioutil"
import "path"
import "fmt"
import "syscall"
import "sync"
import "golang.org/x/crypto/ssh/terminal"

const argFrom = "from"

var fromPath = flag.String(argFrom, "", "File or directory which could be uploaded")

const argTo = "to"

var toPath = flag.String(argTo, "", "Remote directory where everything will be uploaded")

const argHost = "host"

var host = flag.String(argHost, "https://webdav.yandex.ru", "WedDAV server hostname")

const argThreads = "threads"

var threadsNum = flag.Int(argThreads, 10, "Number of threads used for uploading")

const argUser = "user"

var user = flag.String(argUser, "", "Username used for authentication")

const argProfile = "pprof"

var profilingEnabled = flag.Bool(argProfile, false, "Enables profiling")

func main() {
	flag.Parse()
	log.Printf("Arg: %s Value: %s", argFrom, *fromPath)
	log.Printf("Arg: %s Value: %s", argTo, *toPath)
	log.Printf("Arg: %s Value: %d", argThreads, *threadsNum)
	log.Printf("Arg: %s Value: %s", argUser, *user)
	log.Printf("Arg: %s Value: %s", argHost, *host)
	log.Printf("Arg: %s Value: %t", argProfile, *profilingEnabled)

	isCorrectArgs := true
	if fromPath == nil || *fromPath == "" {
		isCorrectArgs = false
	}

	if isCorrectArgs == false {
		log.Printf("Incorrect input arguments")
		flag.PrintDefaults()
		return
	}

	if *profilingEnabled {
		startProfiling()
		defer stopProfiling()
	}

	uploads := createUploadList(*fromPath, *toPath)
	log.Printf("%d files are going to be uploaded", len(uploads))

	if *user == "" {
		*user = requestFromStdin("user")
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

	log.Printf("Finished sending %d tasks\n", len(uploads))
	wg.Wait()
	log.Println("Finished uploading")
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

type UploadSummary struct {
	statuses          map[UploadStatus]int
	totalSizeUploaded int64
	totalTimeSpent    time.Duration
	clockTimeSpent    time.Duration

	failedToUpload []string
}

func NewUploadSummary() *UploadSummary {
	s := &UploadSummary{statuses: make(map[UploadStatus]int, StatusLast),
		failedToUpload: make([]string, 0)}
	return s
}

func (s UploadSummary) print() {
	fmt.Printf("Total uploaded: %d; skipped: %d; failed: %d\n", s.statuses[StatusUploaded], s.statuses[StatusAlreadyExist], s.statuses[StatusFailed])
	fmt.Printf("Total uploaded bytes: %d\n", s.totalSizeUploaded)

	rawSpeed := float64(s.totalSizeUploaded) / s.totalTimeSpent.Seconds()
	fmt.Printf("Raw time spent for upload: %s\n", s.totalTimeSpent)
	fmt.Printf("Raw average speed: %f bytes/s\n", rawSpeed)

	actualSpeed := float64(s.totalSizeUploaded) / s.clockTimeSpent.Seconds()
	fmt.Printf("Clock time spent for upload: %s\n", s.clockTimeSpent)
	fmt.Printf("Average speed: %f bytes/s\n", actualSpeed)

	failedN := len(s.failedToUpload)
	fmt.Printf("Failed uploads: %d\n", failedN)
	if failedN > 0 {
		fname := fmt.Sprintf("upload_failed_%s.list", time.Now().Format("20060102150405"))
		f, err := os.Create(fname)
		defer f.Close()
		if err != nil {
			fmt.Printf("Failed to create file %s for failed uploads. Failed upload list: %+v", fname, s.failedToUpload)
		} else {
			for _, failedUpload := range s.failedToUpload {
				f.WriteString(failedUpload)
				f.WriteString("\n")
			}
			fmt.Printf("Failed uploads have beed written to file %s", fname)
		}
	}
}

func collectResults(results <-chan UploadResult, wg *sync.WaitGroup, resultsExpected int, summary *UploadSummary) {
	for res := range results {
		summary.statuses[res.Status]++
		switch res.Status {
		case StatusUploaded:
			summary.totalSizeUploaded += res.Size
			summary.totalTimeSpent += res.TimeSpent
		case StatusFailed:
			summary.failedToUpload = append(summary.failedToUpload, res.From)
		}
		resultsExpected--
		if resultsExpected == 0 {
			break
		}
	}
	wg.Done()
}
