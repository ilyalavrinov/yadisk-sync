package main

import "log"
import "os"
import "time"
import "crypto/md5"
import "io"
import "path"
import "encoding/hex"
import "github.com/studio-b12/gowebdav"

// UploadOptions provide settings for the connection to the server
// TODO: rename to ConnectionSettings or something similar
type UploadOptions struct {
	Host     string
	User     string
	Password string
	// Token string    // not supported by the library currently
}

// UploadTask defines upload parameters for a single file
type UploadTask struct {
	From, To string
}

// UploadStatus indicates how the upload has finished
type UploadStatus int

// All possible values of UploadStatus
const (
	StatusUploaded     UploadStatus = iota
	StatusFailed                    = iota
	StatusAlreadyExist              = iota

	StatusLast = iota
)

// UploadResult provides the status of the single file upload
type UploadResult struct {
	From      string
	Status    UploadStatus
	TimeSpent time.Duration
	Size      int64
}

// Uploader provides separate goroutine for file upload
type Uploader struct {
	opts    UploadOptions
	client  *gowebdav.Client
	tasks   <-chan UploadTask
	results chan<- UploadResult
}

// NewUploader creates a new Uploader
func NewUploader(opts UploadOptions, tasks <-chan UploadTask, results chan<- UploadResult) *Uploader {
	u := &Uploader{opts: opts,
		client:  gowebdav.NewClient(opts.Host, opts.User, opts.Password),
		tasks:   tasks,
		results: results}

	if err := u.client.Connect(); err != nil {
		log.Fatalf("Could not open connection with settings %+v due to error: %s", u.opts, err)
	}
	return u
}

func calcMD5(localFile string) string {
	f, err := os.Open(localFile)
	if err != nil {
		log.Printf("Could not open %s for hashing; error: %s", localFile, err)
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Printf("Could not calculate hash for %s; error: %s", localFile, err)
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func checkNeedUpload(client *gowebdav.Client, from, to string) bool {
	info, err := client.Stat(to)
	if err != nil {
		if _, ok := err.(*os.PathError); ok {
			log.Printf("PathError for remote file %s. Proceeding with upload", to)
		} else {
			log.Printf("Could not get Stat for remote file %s; error: %s. Uploading still", to, err)
		}
		return true
	}
	if remoteInfo, ok := info.(*gowebdav.File); ok {
		md5remote := remoteInfo.ETag()
		md5local := calcMD5(from)
		if md5remote != md5local {
			log.Printf("md5 of (local) %s and (remote) %s are mismatched: %s != %s", from, to, md5remote, md5local)
			return true
		}
	} else {
		log.Printf("Stat for remote %s is of incorrect type", to)
		return true
	}

	log.Printf("Local file %s has a remote couterpart %s with same md5. No need to upload", from, to)
	return false
}

func uploadOne(client *gowebdav.Client, from, to string) error {
	if !checkNeedUpload(client, from, to) {
		// TODO: return "already exists"
		return nil
	}

	err := client.MkdirAll(path.Dir(to), os.ModePerm)
	if err != nil {
		log.Printf("Error during creation of directories for %s; error: %s", to, err)
		return err
	}

	f, err := os.Open(from)
	if err != nil {
		log.Printf("Error during opening %s: %s", from, err)
		return err
	}
	defer f.Close()
	return client.WriteStream(to, f, os.ModePerm)
}

// Run starts an uploader in a separate goroutine
func (u *Uploader) Run() {
	go func() {
		task, notClosed := <-u.tasks
		for ; notClosed; task, notClosed = <-u.tasks {
			// log.Printf("Uploading %s to %s", task.From, task.To)
			t1 := time.Now()
			err := uploadOne(u.client, task.From, task.To)
			t2 := time.Now()
			if err != nil {
				log.Printf("Could not upload '%s' to '%s' due to error: %s", task.From, task.To, err)
				u.results <- UploadResult{From: task.From,
					Status: StatusFailed}
			} else {
				finfo, _ := os.Stat(task.From)
				tdiff := t2.Sub(t1)
				size := finfo.Size()
				log.Printf("Uploaded %s within %s at speed %f bytes/s", task.From, tdiff, float64(size)/tdiff.Seconds())
				u.results <- UploadResult{Status: StatusUploaded,
					TimeSpent: tdiff,
					Size:      size}
			}
		}
	}()
}
