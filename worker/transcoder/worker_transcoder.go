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

    // Test path ONLY. Need to output to cloud storage such as AWS S3.
    ffmpeg_output_path := "/home/ubuntu/nginx_web_root/output_"
    ffmpeg_output_path += *jobIdPtr
    ffmpeg_output_path += "/"
    err1 = os.Mkdir(ffmpeg_output_path, 0777)
    if err1 != nil {
        fmt.Println("Failed to mkdir: ", ffmpeg_output_path, " Error: ", err1)
        ffmpeg_output_path = "/home/ubuntu/nginx_web_root/"
    }

    ffmpegArgs := job.JobSpecToEncoderArgs(j, ffmpeg_output_path)
    ffmpegCmd := exec.Command("ffmpeg", ffmpegArgs...)

    shutdown := make(chan os.Signal, 1)
    // syscall.SIGKILL cannot be handled
    signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
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