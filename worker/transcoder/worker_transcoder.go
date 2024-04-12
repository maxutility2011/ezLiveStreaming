// Live transcoding/streaming transcoder
package main

import (
	"fmt"
    "os"
	//"strings"
	"encoding/json"
    "os/exec"
    "os/signal"
    "syscall"
    "ezliveStreaming/job"
	"io/ioutil"
    "log"
    "flag"
)

var Log *log.Logger

// worker_transcoder -file job.json 
// worker_transcoder -param [job_json] 
func main() {
    jobIdPtr := flag.String("job_id", "", "input job id")
    jobSpecPathPtr := flag.String("file", "", "input job spec file")
    jobSpecStringPtr := flag.String("param", "", "input job spec string")
    flag.Parse()

    if jobIdPtr != nil {
        fmt.Println("Job id = ", *jobIdPtr)
    }

    var j job.LiveJobSpec
    if *jobSpecStringPtr != "" {
        fmt.Println("Reading job spec from command line argument: ", *jobSpecStringPtr)
        bytesJobSpec := []byte(*jobSpecStringPtr)
        json.Unmarshal(bytesJobSpec, &j)
    } else if *jobSpecPathPtr != "" {
        fmt.Println("Reading job spec from: ", *jobSpecPathPtr)
        jobSpecFile, err := os.Open(*jobSpecPathPtr)
        if err != nil {
            fmt.Println("Failed to open worker_transcoder log file. Error: ", err)
            return
        }

        defer jobSpecFile.Close() 
        bytesJobSpec, _ := ioutil.ReadAll(jobSpecFile)
        json.Unmarshal(bytesJobSpec, &j)
    } else {
        log.Fatal("Error: please provide job spec string or path to job spec file")
        return
    }

    logName := "/tmp/worker_transcoder_" + *jobIdPtr + ".log"
    var logfile, err1 = os.Create(logName)
    if err1 != nil {
        fmt.Println("Exiting... Failed to create log file (worker_transcoder)")
        return
    }

    Log = log.New(logfile, "", log.LstdFlags)

    ffmpegArgs := job.JobSpecToEncoderArgs(j)
    ffmpegCmd := exec.Command("ffmpeg", ffmpegArgs...)

    shutdown := make(chan os.Signal, 1)
    signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
    go func() {
        <-shutdown
        Log.Println("Shutting down!")

        // Received signal from worker_app:
        // - first, stop ffmpeg
        // - then, exit myself
        process, err1 := os.FindProcess(int(ffmpegCmd.Process.Pid))
		if err1 != nil {
        	Log.Printf("Process id = %d (ffmpeg) not found. Error: %v\n", ffmpegCmd.Process.Pid, err1)
    	} else {
			err2 := process.Signal(syscall.Signal(syscall.SIGTERM))
			Log.Printf("process.Signal.SIGTERM on pid %d (ffmpeg) returned: %v\n", ffmpegCmd.Process.Pid, err2)
    	}

        os.Exit(0)
    }()

    out, err2 := ffmpegCmd.CombinedOutput()
    if err2 != nil {
        // error case : status code of command is different from 0
        Log.Println("ffmpeg error: %v", err2, string(out))
    }

    Log.Println("FFmpeg log: ", string(out))
}