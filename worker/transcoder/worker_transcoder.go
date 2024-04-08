// Live transcoding/streaming transcoder
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

var Log *log.Logger

// live_worker -file job.json 
// live_worker -param [job_json] 
func main() {
    var logfile, err1 = os.Create("/tmp/worker.log")
    if err1 != nil {
        panic(err1)
    }

    Log = log.New(logfile, "", log.LstdFlags)
    jobSpecPathPtr := flag.String("file", "", "input job spec file")
    jobSpecStringPtr := flag.String("param", "", "input job spec string")
    flag.Parse()

    var j job.LiveJobSpec
    if *jobSpecStringPtr != "" {
        Log.Println("Reading job spec from command line argument: ", *jobSpecStringPtr)
        bytesJobSpec := []byte(*jobSpecStringPtr)
        json.Unmarshal(bytesJobSpec, &j)
    } else if *jobSpecPathPtr != "" {
        Log.Println("Reading job spec from: ", *jobSpecPathPtr)
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

    fmt.Println("Input Url: ", j.Input.Url)
    ffmpegArgs := job.JobSpecToEncoderArgs(j)
    out, err2 := exec.Command("ffmpeg", ffmpegArgs...).CombinedOutput()
    if err2 != nil {
        // error case : status code of command is different from 0
        fmt.Println("ffmpeg error: %v", err2, string(out))
    }
}