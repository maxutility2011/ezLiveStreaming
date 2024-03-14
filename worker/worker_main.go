package main

import (
	"fmt"
    "os"
	//"strings"
	"encoding/json"
    "os/exec"
    "ezliveStreaming/job"
	"io/ioutil"
    "log"
    "flag"
)

// live_worker -f job.json 
// live_worker -p [job_json] 
func main() {
    var logfile, err1 = os.Create("/tmp/worker.log")
    if err1 != nil {
        panic(err1)
    }

    Log := log.New(logfile, "", log.LstdFlags|log.Lshortfile)

    jobSpecPathPtr := flag.String("file", "", "input job spec file")
    jobSpecStringPtr := flag.String("param", "", "input job spec string")
    Log.Println("jobSpecStringPtr: ", *jobSpecStringPtr)
    Log.Println("jobSpecPathPtr: ", *jobSpecPathPtr)

    flag.Parse()

    var j job.LiveJobSpec

    if *jobSpecStringPtr != "" {
        bytesJobSpec := []byte(*jobSpecStringPtr)
        json.Unmarshal(bytesJobSpec, &j)
    } else if *jobSpecPathPtr != "" {
        jobSpecFile, err := os.Open(*jobSpecPathPtr)
        if err != nil {
            fmt.Println(err)
        }

        defer jobSpecFile.Close() 
        bytesJobSpec, _ := ioutil.ReadAll(jobSpecFile)
        json.Unmarshal(bytesJobSpec, &j)
    } else {
        log.Fatal("Error: please provide job spec string or path to job spec file")
        return
    }

    Log.Println("Input Url: ", j.Input.Url)

    ffmpegArgs := job.JobSpecToEncoderArgs(j)
    out, err2 := exec.Command("ffmpeg", ffmpegArgs...).CombinedOutput()
    if err2 != nil {
        // error case : status code of command is different from 0
        log.Fatal("ffmpeg error: %v", err2, string(out))
    }
}