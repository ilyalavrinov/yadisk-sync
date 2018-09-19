package main

import "os"
import "path"
import "log"
import "runtime"
import "runtime/pprof"
import "math/rand"
import "time"
import "strconv"

var pprofDir string

func startProfiling() {
    if pprofDir != "" {
        panic("profiling directory is already set")
    }

    rand.Seed(int64(time.Now().Nanosecond()))
    pprofDir = path.Join(os.TempDir(), "yadiskprofile", strconv.Itoa(int(100000 + rand.Int31n(899999))))

    log.Printf("Starting profiling. Everything will be stored at %s \n", pprofDir)
    err := os.MkdirAll(pprofDir, os.ModePerm)
    if err != nil {
        panic(err)
    }

    cpuFileName := path.Join(pprofDir, "cpu.pprof")
    cpuFile, err := os.Create(cpuFileName)
    if err != nil {
        panic(err)
    }

    err = pprof.StartCPUProfile(cpuFile)
    if err != nil {
        panic(err)
    }
}

func stopProfiling() {
    pprof.StopCPUProfile()

    if pprofDir == "" {
        log.Printf("Profile dir is empty. Some profiling will be not written")
        return
    }

    memFileName := path.Join(pprofDir, "mem.pprof")
    memFile, err := os.Create(memFileName)
    if err != nil {
        panic(err)
    }
    runtime.GC() // get up-to-date statistics
    if err := pprof.WriteHeapProfile(memFile); err != nil {
        panic(err)
    }
    memFile.Close()
    log.Printf("Profiling has been finished. All data is at %s \n", pprofDir)
}
