// Live transcoding/streaming transcoder
package main

import (
	"fmt"
    "os"
    "time"
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

var transcoder_status_check_interval = "2s"

func checkPackagerStatus() {

}

func checkFfmpegStatus() {

}

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
            fmt.Println("Failed to open worker_transcoder spec file. Error: ", err)
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
    media_output_path := "/var/www/html/" + job.Media_output_path_prefix
    media_output_path += *jobIdPtr
    media_output_path += "/"
    err1 = os.Mkdir(media_output_path, 0777)
    if err1 != nil {
        Log.Println("Failed to mkdir: ", media_output_path, " Error: ", err1)
        os.Exit(1)
    }

    // Start Shaka packager first
    packagerArgs := job.JobSpecToShakaPackagerArgs(j, media_output_path)
    Log.Println("Shaka packager arguments: ")
    Log.Println(job.ArgumentArrayToString(packagerArgs))
    //os.Exit(0) // unit test 

    // TODO: File path of the packager binary needs to be added to the PATH env-var
    packagerCmd := exec.Command("packager", packagerArgs...)

    var out []byte
	var err2 error
	err2 = nil
	go func() {
		out, err2 = packagerCmd.CombinedOutput() // This line blocks when packagerCmd launch succeeds
		if err2 != nil {
			//running_jobs.Remove(je) // Cleanup if packagerCmd fails
        	Log.Println("Errors starting Shaka packager: ", string(out))
            os.Exit(1)
		}
	}()

    // Wait 100ms before Shaka packager fully starts
    time.Sleep(100 * time.Millisecond)
    if (err2 != nil) {
        Log.Println("Errors starting Shaka packager: ", string(out))
        os.Exit(1)
    }

    // Start ffmpeg ONLY if Shaka packager is running
    ffmpegArgs := job.JobSpecToFFmpegArgs(j, media_output_path)
    Log.Println("FFmpeg arguments: ")
    Log.Println(job.ArgumentArrayToString(ffmpegArgs))

    ffmpegCmd := exec.Command("ffmpeg", ffmpegArgs...)

	err2 = nil
	go func() {
		out, err2 = ffmpegCmd.CombinedOutput() // This line blocks when ffmpegCmd launch succeeds
		if err2 != nil {
			//running_jobs.Remove(je) // Cleanup if ffmpegCmd fails
        	Log.Println("Errors starting ffmpeg: ", string(out))
            os.Exit(1)
		}
	}()

    // Wait 100ms before ffmpeg fully starts
    time.Sleep(100 * time.Millisecond)
    if (err2 != nil) {
        Log.Println("Errors starting ffmpeg: ", string(out))
        os.Exit(1)
    }

    // Handle system signals to terminate worker_transcoder
    shutdown := make(chan os.Signal, 1)
    // syscall.SIGKILL cannot be handled
    signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
    go func() {
        <-shutdown
        Log.Println("worker_transcoder shutting down!")

        // Received signal from worker_app:
        // - first, stop shaka packager and ffmpeg
        // - then, exit myself
        processPackager, err3 := os.FindProcess(int(packagerCmd.Process.Pid))
		if err3 != nil {
        	Log.Printf("Process id = %d (packagerCmd) not found. Error: %v\n", packagerCmd.Process.Pid, err3)
    	} else {
			err3 = processPackager.Signal(syscall.Signal(syscall.SIGTERM))
			Log.Printf("process.Signal.SIGTERM on pid %d (Shaka packager) returned: %v\n", packagerCmd.Process.Pid, err3)
    	}

        processFfmpeg, err4 := os.FindProcess(int(ffmpegCmd.Process.Pid))
		if err4 != nil {
        	Log.Printf("Process id = %d (ffmpeg) not found. Error: %v\n", ffmpegCmd.Process.Pid, err4)
    	} else {
			err4 = processFfmpeg.Signal(syscall.Signal(syscall.SIGTERM))
			Log.Printf("process.Signal.SIGTERM on pid %d (ffmpeg) returned: %v\n", ffmpegCmd.Process.Pid, err4)
    	}

        os.Exit(0)
    }()

    /*
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
    */

    d, _ := time.ParseDuration(transcoder_status_check_interval)
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
		   select {
			    case <-ticker.C: {
				    checkPackagerStatus()
                    checkFfmpegStatus()
			    }
			    case <-quit: {
				    ticker.Stop()
                    os.Exit(0)
                }
			}
		}
	}(ticker)

    <-quit
}
