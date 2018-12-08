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

// OperationType determines what type of operation has to be done
type OperationType int

// All available task types
const (
	OperationUpload   OperationType = iota
	OperationDownload OperationType = iota
)

// TransferTask defines transfer parameters for a single file
type TransferTask struct {
	Operation OperationType
	From, To  string
}

// TransferStatus indicates how the transfer has finished
type TransferStatus int

// All possible values of TransferStatus
const (
	StatusDone         TransferStatus = iota
	StatusFailed                      = iota
	StatusAlreadyExist                = iota

	StatusLast = iota
)

// TransferResult provides the status of the single file transfer
type TransferResult struct {
	Task      TransferTask
	Status    TransferStatus
	TimeSpent time.Duration
	Size      int64
	Error     error
}

// Worker provides separate goroutine for file transfer
type Worker struct {
	opts    TransferSettings
	client  *gowebdav.Client
	tasks   <-chan TransferTask
	results chan<- TransferResult
}

// NewWorker creates a new Worker
func NewWorker(opts TransferSettings, tasks <-chan TransferTask, results chan<- TransferResult) *Worker {
	u := &Worker{opts: opts,
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

func downloadOne(client *gowebdav.Client, from, to string) (TransferStatus, error) {
	if !checkNeedUpload(client, to, from) {
		return StatusAlreadyExist, nil
	}

	remote, err := client.ReadStream(from)
	if err != nil {
		log.WithFields(log.Fields{"file": from, "error": err}).Error("Could not open remote file for reading")
		return StatusFailed, err
	}

	dirs := path.Dir(to)
	err = os.MkdirAll(dirs, os.ModePerm)
	if err != nil {
		log.WithFields(log.Fields{"dir": dirs, "error": err}).Error("Could not create local directories")
		return StatusFailed, err
	}

	local, err := os.Create(to)
	if err != nil {
		log.WithFields(log.Fields{"file": to, "error": err}).Error("Could not open local file for writing")
		return StatusFailed, err
	}

	_, err = io.Copy(local, remote)
	if err != nil {
		log.WithFields(log.Fields{"local": to, "remote": from, "error": err}).Error("Could not copy remote file into a local")
		return StatusFailed, err
	}

	return StatusDone, nil
}

// Run starts a worker in a separate goroutine
func (u *Worker) Run() {
	go func() {
		task, notClosed := <-u.tasks
		for ; notClosed; task, notClosed = <-u.tasks {

			var status TransferStatus
			var err error
			var localFile string
			t1 := time.Now()
			switch task.Operation {
			case OperationUpload:
				status, err = uploadOne(u.client, task.From, task.To)
				localFile = task.From
			case OperationDownload:
				status, err = downloadOne(u.client, task.From, task.To)
				localFile = task.To
			default:
				panic("Unexpected operation received")
			}

			tdiff := time.Now().Sub(t1)
			finfo, _ := os.Stat(localFile)
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
