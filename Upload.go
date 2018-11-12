package main

import log "github.com/sirupsen/logrus"
import "os"
import "time"
import "crypto/md5"
import "io"
import "path"
import "encoding/hex"
import "github.com/studio-b12/gowebdav"

// TransferSettings provide settings for the connection to the server
// TODO: rename to ConnectionSettings or something similar
type TransferSettings struct {
	Host     string
	User     string
	Password string
	// Token string    // not supported by the library currently
}

// TransferTask defines upload parameters for a single file
type TransferTask struct {
	From, To string
}

// TransferStatus indicates how the upload has finished
type TransferStatus int

// All possible values of TransferStatus
const (
	StatusDone         TransferStatus = iota
	StatusFailed                      = iota
	StatusAlreadyExist                = iota

	StatusLast = iota
)

// TransferResult provides the status of the single file upload
type TransferResult struct {
	Task      TransferTask
	Status    TransferStatus
	TimeSpent time.Duration
	Size      int64
	Error     error
}

// Uploader provides separate goroutine for file upload
type Uploader struct {
	opts    TransferSettings
	client  *gowebdav.Client
	tasks   <-chan TransferTask
	results chan<- TransferResult
}

// NewUploader creates a new Uploader
func NewUploader(opts TransferSettings, tasks <-chan TransferTask, results chan<- TransferResult) *Uploader {
	u := &Uploader{opts: opts,
		client:  gowebdav.NewClient(opts.Host, opts.User, opts.Password),
		tasks:   tasks,
		results: results}

	if err := u.client.Connect(); err != nil {
		log.WithFields(log.Fields{"settings": u.opts, "error": err}).Fatal("Could not open connection")
	}
	return u
}

func calcMD5(localFile string) string {
	f, err := os.Open(localFile)
	if err != nil {
		log.WithFields(log.Fields{"file": localFile, "error": err}).Debug("Could not open file for hashing")
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		log.WithFields(log.Fields{"file": localFile, "error": err}).Debug("Could not calculate hash")
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func checkNeedUpload(client *gowebdav.Client, from, to string) bool {
	info, err := client.Stat(to)
	if err != nil {
		if _, ok := err.(*os.PathError); ok {
			log.WithFields(log.Fields{"file": to}).Debug("PathError for remote file (ok, file is absent)")
		} else {
			log.WithFields(log.Fields{"file": to, "error": err}).Debug("Some error for remote file (still need uploading)")
		}
		return true
	}
	if remoteInfo, ok := info.(*gowebdav.File); ok {
		md5remote := remoteInfo.ETag()
		md5local := calcMD5(from)
		if md5remote != md5local {
			log.WithFields(log.Fields{
				"file_local":  from,
				"file_remote": to,
				"md5_local":   md5local,
				"md5_remote":  md5remote}).Debug("MD5 mismatch for local and remote")
			return true
		}
	} else {
		log.WithFields(log.Fields{"file_remote": to}).Error("Stat for remote file has unexpected type (still uploading)")
		return true
	}

	log.WithFields(log.Fields{"file_local": from, "file_remote": to}).Debug("Local and remote have same MD5")
	return false
}

func uploadOne(client *gowebdav.Client, from, to string) (TransferStatus, error) {
	if !checkNeedUpload(client, from, to) {
		// TODO: return "already exists"
		return StatusAlreadyExist, nil
	}

	err := client.MkdirAll(path.Dir(to), os.ModePerm)
	if err != nil {
		log.WithFields(log.Fields{"file": to, "error": err}).Error("Could not create directory tree")
		return StatusFailed, err
	}

	f, err := os.Open(from)
	if err != nil {
		log.WithFields(log.Fields{"file": from, "error": err}).Error("Could not open local file")
		return StatusFailed, err
	}
	defer f.Close()

	err = client.WriteStream(to, f, os.ModePerm)
	if err != nil {
		log.WithFields(log.Fields{"file": from, "error": err}).Error("Could not upload file")
		return StatusFailed, err
	}
	return StatusDone, nil
}

// Run starts an uploader in a separate goroutine
func (u *Uploader) Run() {
	go func() {
		task, notClosed := <-u.tasks
		for ; notClosed; task, notClosed = <-u.tasks {
			// log.Printf("Uploading %s to %s", task.From, task.To)
			t1 := time.Now()
			status, err := uploadOne(u.client, task.From, task.To)
			tdiff := time.Now().Sub(t1)
			finfo, _ := os.Stat(task.From)
			size := finfo.Size()
			res := TransferResult{
				Status:    status,
				Task:      task,
				TimeSpent: tdiff,
				Size:      size,
				Error:     err}
			u.results <- res
		}
	}()
}
