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
    //"bytes"
)

// live_worker -f job.json 
// live_worker -p [job_json] 
func main() {
    jobSpecPathPtr := flag.String("file", "job.json", "input job spec file")
    //jobSpecStringPtr := flag.String("p", "", "input job spec string")

    flag.Parse()

    jobSpecFile, err := os.Open(*jobSpecPathPtr)
    if err != nil {
        fmt.Println(err)
    }

    defer jobSpecFile.Close() 
    var j job.LiveJobSpec
    bytesJobSpec, _ := ioutil.ReadAll(jobSpecFile)
    json.Unmarshal(bytesJobSpec, &j)
    fmt.Println("Input Url: ", j.Input.Url)

    //var ffmpegArgs []string
    //ffmpegArgs = append(ffmpegArgs, "-i")
    //ffmpegArgs = append(ffmpegArgs, "2.mp4")
    //ffmpegArgs = append(ffmpegArgs, "out.mp4")

    ffmpegArgs := job.JobSpecToEncoderArgs(j)
    out, err2 := exec.Command("ffmpeg", ffmpegArgs...).CombinedOutput()
    if err2 != nil {
        // error case : status code of command is different from 0
        log.Fatal("ffmpeg error: %v", err2, string(out))
    }

    //fmt.Println(cmdErr.String())
}