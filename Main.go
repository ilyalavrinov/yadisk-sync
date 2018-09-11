package main

import "log"
import "flag"
import "os"
import "io/ioutil"
import "path"

const argFrom = "from"
var fromPath = flag.String(argFrom, "", "File or directory which could be uploaded")

const argTo = "to"
var toPath = flag.String(argTo, "/", "Remote directory where everything will be uploaded")

func main() {
    flag.Parse()
    log.Printf("Arg: %s Value: %s", argFrom, *fromPath)
    log.Printf("Arg: %s Value: %s", argTo, *toPath)

    isCorrectArgs := true
    if fromPath == nil || *fromPath == "" {
        isCorrectArgs = false
    }
    if *toPath == "" {
        isCorrectArgs = false
    }

    if isCorrectArgs == false {
        log.Printf("Incorrect input arguments")
        flag.PrintDefaults()
        return
    }

    uploads := createUploadList(*fromPath, *toPath)
    log.Printf("Uploads: %+v", uploads)
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
