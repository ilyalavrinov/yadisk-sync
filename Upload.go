package main

import "log"
import "os"
import "time"
import "github.com/studio-b12/gowebdav"

type UploadOptions struct {
    Host string
    User string
    Password string
    // Token string    // not supported by the library currently
}

type UploadTask struct {
    From, To string
}

type UploadStatus int
const (
    StatusUploaded UploadStatus = iota
    StatusFailed = iota
    StatusAlreadyExist = iota

    StatusLast = iota
)

type UploadResult struct {
    From string
    Status UploadStatus
    TimeSpent time.Duration
    Size int64
}

type Uploader struct {
    opts UploadOptions
    client gowebdav.Client
    tasks <-chan UploadTask
    results chan<- UploadResult
}

func NewUploader(opts UploadOptions, tasks <-chan UploadTask, results chan<- UploadResult) *Uploader {
    u := &Uploader{opts: opts,
                   client: *gowebdav.NewClient(opts.Host, opts.User, opts.Password),
                   tasks: tasks,
                   results: results}

    if err := u.client.Connect(); err != nil {
        log.Fatalf("Could not open connection with settings %+v due to error: %s", u.opts, err)
    }
    return u
}

func uploadOne(client *gowebdav.Client, from, to string) error {
    f, err := os.Open(from)
    if err != nil {
        log.Printf("Error during opening %s: %s", from, err)
        return err
    }
    return client.WriteStream(to, f, os.ModePerm)
}

func (u *Uploader) Run() {
    go func() {
        task, notClosed := <-u.tasks
        for ; notClosed; task, notClosed = <-u.tasks {
            log.Printf("Uploading %s to %s", task.From, task.To)
            t1 := time.Now()
            err := uploadOne(&u.client, task.From, task.To)
            t2 := time.Now()
            log.Printf("Uploaded %s with error: %v", task.From, err)
            if err != nil {
                log.Printf("Could not upload '%s' to '%s' due to error: %s", task.From, task.To, err)
                u.results<- UploadResult{From: task.From,
                                         Status: StatusFailed}
            } else {
                finfo, _ := os.Stat(task.From)
                u.results<- UploadResult{Status: StatusUploaded,
                                         TimeSpent: t2.Sub(t1),
                                         Size: finfo.Size()}
            }
        }
    }()
}
