package main

import "log"

type UploadOptions struct {
    Token string
}

type UploadTask struct {
    From, To string
}

type Uploader struct {
    opts UploadOptions
    ch <-chan UploadTask
}

func NewUploader(opts UploadOptions, tasks <-chan UploadTask) *Uploader {
    u := &Uploader{opts: opts,
                   ch: tasks}
    return u
}

func uploadOne(token string, from, to string) error {
    panic("Not implemented")
}

func (u *Uploader) Run() {
    go func() {
        task, notClosed := <-u.ch
        for ; notClosed; task, notClosed = <-u.ch {
            err := uploadOne(u.opts.Token, task.From, task.To)
            if err != nil {
                log.Printf("Could not upload '%s' to '%s' due to error: %s", task.From, task.To, err)
            }
        }
    }()
}
