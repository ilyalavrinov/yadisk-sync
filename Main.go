package main

import "log"
import "flag"
import "os"
import "time"
import "io/ioutil"
import "path"
import "fmt"
import "syscall"
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

func main() {
    flag.Parse()
    log.Printf("Arg: %s Value: %s", argFrom, *fromPath)
    log.Printf("Arg: %s Value: %s", argTo, *toPath)
    log.Printf("Arg: %s Value: %d", argThreads, *threadsNum)
    log.Printf("Arg: %s Value: %s", argUser, *user)
    log.Printf("Arg: %s Value: %s", argHost, *host)

    isCorrectArgs := true
    if fromPath == nil || *fromPath == "" {
        isCorrectArgs = false
    }
    if *user == "" {
        // TODO: request from stdin same as password
        isCorrectArgs = false
    }

    if isCorrectArgs == false {
        log.Printf("Incorrect input arguments")
        flag.PrintDefaults()
        return
    }

    uploads := createUploadList(*fromPath, *toPath)
    log.Printf("Uploads: %+v", uploads)

    pw := requestFromStdin("password")
    opts := UploadOptions { Host: *host,
                            User: *user,
                            Password: pw }

    uploadTasks := make(chan UploadTask, *threadsNum * 100)
    for i := 0; i < *threadsNum; i++ {
        uploader := NewUploader(opts, uploadTasks)
        uploader.Run()
    }

    for _, u := range uploads {
        uploadTasks<- u
    }

    log.Printf("Finished sending tasks")
    time.Sleep(1 * time.Minute)
}

func createUploadList(fpath, uploadDir string) []UploadTask {
    result := make([]UploadTask, 0, 1)
    log.Printf("Creating upload list for: %s (with uploadDir %s)", fpath, uploadDir)
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
    fmt.Print("Enter Password: ")
    bytePassword, _ := terminal.ReadPassword(int(syscall.Stdin))
    return string(bytePassword)
}
