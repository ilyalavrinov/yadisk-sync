package main

import "log"
import "os"
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

type Uploader struct {
    opts UploadOptions
    client gowebdav.Client
    ch <-chan UploadTask
}

func NewUploader(opts UploadOptions, tasks <-chan UploadTask) *Uploader {
    u := &Uploader{opts: opts,
                   client: *gowebdav.NewClient(opts.Host, opts.User, opts.Password),
                   ch: tasks}

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
        task, notClosed := <-u.ch
        for ; notClosed; task, notClosed = <-u.ch {
            log.Printf("Uploading %s to %s", task.From, task.To)
            err := uploadOne(&u.client, task.From, task.To)
            if err != nil {
                log.Printf("Could not upload '%s' to '%s' due to error: %s", task.From, task.To, err)
            }
        }
    }()
}
