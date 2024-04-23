package main

import (
	"fmt"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
	"net/http"
	"bytes"
	"flag"
	"strings"
	"log"
	"strconv"
	"io/ioutil"
	"encoding/json"
	"container/list"
	"ezliveStreaming/models"
	"ezliveStreaming/job"
)

type WorkerAppConfig struct {
	SchedulerUrl string
	WorkerAppIp string
	WorkerAppPort string
}

type RunningJob struct {
	Job job.LiveJob
	Command *exec.Cmd
}

var liveJobsEndpoint = "jobs"

func createJob(j job.LiveJob) (error, string) {
	j.Time_received_by_worker = time.Now()
	e := createUpdateJob(j)
	if e != nil {
		fmt.Println("Error: Failed to create/update job ID: ", j.Id)
		return e, ""
	}

	j2, ok := getJobById(j.Id) 
	if !ok {
		fmt.Println("Error: Failed to find job ID: ", j.Id)
		return e, ""
	} 

	fmt.Printf("New job created: %+v\n", j2)
	return nil, j.Id
}

func createUpdateJob(j job.LiveJob) error {
	jobs[j.Id] = j
	return nil
}

func getJobById(jid string) (job.LiveJob, bool) {
	job, ok := jobs[jid]
	return job, ok
}

func deleteJob(jid string) error {
	delete(jobs, jid) 
	return nil
}

func stopJob(jid string) error {
	_, ok := getJobById(jid)
	if !ok {
		fmt.Println("Cannot stop non-existent job id = ", jid)
		return errors.New("jobNotFound") // return error and return "404 not found" to scheduler. Do not retry in this case
	}

	jobFound := false
	stopped := false
	for e := running_jobs.Front(); e != nil; e = e.Next() {
		j := RunningJob(e.Value.(RunningJob))
		if j.Job.Id != jid {
			continue
		}

		jobFound = true
		process, err1 := os.FindProcess(int(j.Command.Process.Pid))
		if err1 != nil {
        	fmt.Printf("Process id = %d (Job id = %s) not found in stopJob. Error: %v\n", j.Command.Process.Pid, j.Job.Id, err1)
    	} else {
			err2 := process.Signal(syscall.Signal(syscall.SIGTERM))
			fmt.Printf("process.Signal.SIGTERM on pid %d (Job id = %s) returned: %v\n", j.Command.Process.Pid, j.Job.Id, err2)
			if err2 == nil {
				releaseRtmpPort(j.Job.RtmpIngestPort)
				deleteJob(jid) // Delete the job from this worker. The job could be reassigned to another worker when it resumes.
				stopped = true
			}
		}

		break
	}

	// Can't stop a non-existent or already stopped job
	if !jobFound {
		fmt.Println("Cannot stop the job. Is job id = ", jid, " running?")
		return errors.New("jobNotFound") // return error and return "404 not found" to scheduler. Do not retry in this case
	}

	var e error
	if stopped {
		e = nil
	} else {
		e = errors.New("StopJobFailed") // return error and let scheduler retry
	}

	return e
}

func main_server_handler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("----------------------------------------")
    fmt.Println("Received new request:")
    fmt.Println(r.Method, r.URL.Path)

    posLastSingleSlash := strings.LastIndex(r.URL.Path, "/")
    UrlLastPart := r.URL.Path[posLastSingleSlash + 1 :]

    // Remove trailing "/" if any
    if len(UrlLastPart) == 0 {
        path_without_trailing_slash := r.URL.Path[0 : posLastSingleSlash]
        posLastSingleSlash = strings.LastIndex(path_without_trailing_slash, "/")
        UrlLastPart = path_without_trailing_slash[posLastSingleSlash + 1 :]
    } 

	if strings.Contains(r.URL.Path, liveJobsEndpoint) {
		if !(r.Method == "POST" || r.Method == "GET" || r.Method == "PUT" || r.Method == "DELETE") {
            err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
            fmt.Println(err)
            http.Error(w, "405 method not allowed\n  Error: " + err, http.StatusMethodNotAllowed)
            return
        }

		if r.Method == "POST" && UrlLastPart != liveJobsEndpoint {
			res := "POST to " + r.URL.Path + "is not allowed"
			fmt.Println(res)
			http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
		} else if r.Method == "POST" && UrlLastPart == liveJobsEndpoint {
			if r.Body == nil {
            	res := "Error New live job without job specification"
            	fmt.Println("Error New live job without job specifications")
            	http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            	return
        	}

			var job job.LiveJob
			e := json.NewDecoder(r.Body).Decode(&job)
			if e != nil {
            	res := "Failed to decode job request"
            	fmt.Println("Error happened in JSON marshal. Err: %s", e)
            	http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            	return
        	}

			e1, jid := createJob(job)
			if e1 != nil {
				deleteJob(jid)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			createIngestUrl(job)
			j, ok := getJobById(jid)
			if ok {
				e3 := launchJob(j)
				if e3 != nil {
					fmt.Println("Failed to launch job id =", jid, " Error: ", e3)
					deleteJob(jid)
					http.Error(w, "500 Internal serve error\n  Error: ", http.StatusInternalServerError)
					return
				}

				FileContentType := "application/json"
				w.Header().Set("Content-Type", FileContentType)
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(j)
			} else {
				fmt.Println("Failed to get job id = ", jid, " (worker_app.main_server_handler)")
				deleteJob(jid)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
			}
		} else if r.Method == "GET" {
			// Get all jobs: /jobs/
			if UrlLastPart == liveJobsEndpoint {
				FileContentType := "application/json"
        		w.Header().Set("Content-Type", FileContentType)
        		w.WriteHeader(http.StatusOK)
        		json.NewEncoder(w).Encode(jobs)
				return
			} else { // Get one job: /jobs/[job_id]
				job, ok := jobs[UrlLastPart]
				if ok {
					FileContentType := "application/json"
        			w.Header().Set("Content-Type", FileContentType)
        			w.WriteHeader(http.StatusOK)
        			json.NewEncoder(w).Encode(job)
				} else {
					fmt.Println("Non-existent job id: ", UrlLastPart)
                    http.Error(w, "Non-existent job id: " + UrlLastPart, http.StatusNotFound)
				}

				return
			}
		} else if r.Method == "PUT" {
			if strings.Contains(r.URL.Path, "stop") {
				begin := strings.Index(r.URL.Path, "jobs") + len("jobs") + 1
				end := strings.Index(r.URL.Path, "stop") - 1
				jid := r.URL.Path[begin:end]

				e := stopJob(jid)
				if e == nil {
					fmt.Println("Job id = ", jid, " is successfully stopped")
					w.WriteHeader(http.StatusOK)
				} else if e.Error() == "jobNotFound" {
					http.Error(w, "404 internal server error\n  Error: ", http.StatusNotFound)
				}else {
					fmt.Println("Job id = ", jid, " failed to stop")
					http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				}

				return
			}
		}
	}
}

func readConfig() {
	configFile, err := os.Open(worker_app_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close() 
	config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(config_bytes, &worker_app_config)
}

//var rtmp_ingest_port = 1935
var rtmp_port_base = 1935 // TODO: Make this configurable
var max_rtmp_ports = 15 // TODO: Make this configurable
var available_rtmp_ports *list.List
var scheduler_heartbeat_interval = "1s" 
var job_status_check_interval = "2s"
var worker_app_config_file_path = "worker_app_config.json"
var Log *log.Logger
var job_scheduler_url string
// TODO: Need to move "jobs" to Redis or find a way to recover jobs when worker_app crashes
// TODO: Need to constantly monitor job health. Need to re-assign a job to a new worker
//       when the existing worker crashes.
var jobs = make(map[string]job.LiveJob) // Live jobs assigned to this worker
var running_jobs *list.List // Live jobs actively running on this worker
var myWorkerId string
var last_confirmed_heartbeat_time time.Time
var worker_app_config WorkerAppConfig

func getCpuCapacity() string {
	return "5000"
}

func getBandwidthCapacity() string {
	return "100m"
}

func allocateRtmpIngestPort() int {
	var port int
	e := available_rtmp_ports.Front()
	if e != nil {
		port = int(e.Value.(int))
		available_rtmp_ports.Remove(e)
	} else {
		port = -1
	}

	return port
}

// Need to release RTMP port when a job is done
func releaseRtmpPort(port int) {
	fmt.Println("Releasing RTMP port: ", port)
	available_rtmp_ports.PushBack(port)
}

func createIngestUrl(job job.LiveJob) error {
	rtmp_ingest_port := allocateRtmpIngestPort() // TODO: need to support RTMPS
	// srt_ingest_port := allocateSrtIngestPort() // TODO
	// srt_ingest_port := allocateRtpIngestPort() // TODO
	var err error
	err = nil
	if rtmp_ingest_port < 0 {
		err = errors.New("NotEnoughRtmpIngestPort")
	} else {
		job.RtmpIngestPort = rtmp_ingest_port
		job.RtmpIngestUrl = "rtmp://" + "0.0.0.0" + ":" + strconv.Itoa(rtmp_ingest_port) + "/live/" + job.StreamKey
		// job.SrtIngestUrl = ...
		// job.RtpIngestUrl = ...
	}

	createUpdateJob(job)
	return err
}

func launchJob(j job.LiveJob) error {
	j.Spec.Input.Url = j.RtmpIngestUrl
	b, err := json.Marshal(j.Spec)
	if err != nil {
		fmt.Println("Failed to marshal job output (launchJob). Error: ", err)
		return err
	}
	
	var transcoderArgs []string

	jobIdArg := "-job_id="
	jobIdArg += j.Id
	transcoderArgs = append(transcoderArgs, jobIdArg)

	paramArg := "-param="
	paramArg += string(b[:])
	transcoderArgs = append(transcoderArgs, paramArg)

	fmt.Println("Worker arguments: ", strings.Join(transcoderArgs, " "))
	ffmpegCmd := exec.Command("worker_transcoder", transcoderArgs...)

	var rj RunningJob
	rj.Job = j
	rj.Command = ffmpegCmd
	je := running_jobs.PushBack(rj)

	var out []byte
	var err2 error
	err2 = nil
	go func() {
		out, err2 = ffmpegCmd.CombinedOutput() // This line blocks when ffmpegCmd launch succeeds
		if err2 != nil {
			running_jobs.Remove(je) // Cleanup if ffmpegCmd fails
        	fmt.Println("Errors running worker transcoder: ", string(out))
		}
	}()

	// Wait 100ms to get ffmpegCmd (which runs in its own go routine) result (err2). 

	// If we don't wait, err2=nil (its initial value) will always be returned before 
	// ffmpegCmd.CombinedOutput() finishes and returns the real result. This will cause inconsistency
	// when launchJob() returns nil (success) but ffmpegCmd.CombinedOutput() actually fails. 

	// On the other hand, if ffmpegCmd succeeds, the go routine (ffmpegCmd.CombinedOutput) blocks until 
	// ffmpeg stops (e.g., when the user issues a stop_job request). In this case, err2 will not be 
	// updated when the sleep ends thus err2=nil will be returned (which is valid). 
	time.Sleep(100 * time.Millisecond)
	return err2
}

func reportJobStatus(report models.WorkerJobReport) error {
	Log.Println("Sending job status report at time =", time.Now())
	job_status_url := job_scheduler_url + "/" + "jobstatus"

	b, _ := json.Marshal(report)
	req, err := http.NewRequest(http.MethodPost, job_status_url, bytes.NewReader(b))
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", job_status_url)
		// TODO: Need to retry registering new worker instead of giving up
        return errors.New("StatusReportFailure_fail_to_post")
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        fmt.Println("Failed to POST job status: ", job_status_url)
		return err
    }
	
	/*
    defer resp.Body.Close()
    bodyBytes, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Println("Error: Failed to read response body (reportJobStatus)")
        return errors.New("StatusReportFailure_fail_to_read_scheduler_response")
    }

	var hb_resp models.WorkerHeartbeat
	json.Unmarshal(bodyBytes, &hb_resp)
	*/

	// TODO: Need to handle error response (other than http code 200)
	if resp.StatusCode != http.StatusOK {
		fmt.Println("Bad response from scheduler (reportJobStatus)")
        return errors.New("StatusReportFailure_fail_to_read_scheduler_response")
	}

	return nil
}

func checkIngestActiveness() {
	
}

func checkJobStatus() {
	//fmt.Println("Checking job status... Number of running jobs = ", running_jobs.Len())
	var job_report models.WorkerJobReport
	var prev_e *list.Element
	var jobProcessFound bool
	var jobProcessRunning bool

	job_report.WorkerId = myWorkerId
	prev_e = nil
	jobProcessFound = false
	jobProcessRunning = false
	var j RunningJob
	for e := running_jobs.Front(); e != nil; e = e.Next() {
		// Remove stopped jobs and update scheduler
		if (prev_e != nil && (!jobProcessFound || !jobProcessRunning)) {
			j = RunningJob(prev_e.Value.(RunningJob))
			job_report.StoppedJobs = append(job_report.StoppedJobs, j.Job.Id)
			running_jobs.Remove(prev_e)
		}

		j = RunningJob(e.Value.(RunningJob))
		jobProcessFound = true
		jobProcessRunning = true
		var err2 error

		if j.Command == nil {
			fmt.Println("Skip checking partially launched job!")
			prev_e = e
			continue
		}

		process, err1 := os.FindProcess(int(j.Command.Process.Pid))
		if err1 != nil {
            fmt.Printf("Process id = %d (Job id = %s) not found. Error: %v\n", j.Command.Process.Pid, j.Job.Id, err1)
			jobProcessFound = false
        } else {
			err2 = process.Signal(syscall.Signal(0))
			fmt.Printf("process.Signal on pid %d (Job id = %s) returned: %v\n", j.Command.Process.Pid, j.Job.Id, err2)
			if err2 != nil {
				jobProcessRunning = false
			}
        }

		prev_e = e
	}

	if (prev_e != nil && (!jobProcessFound || !jobProcessRunning)) {
		j = RunningJob(prev_e.Value.(RunningJob))
		job_report.StoppedJobs = append(job_report.StoppedJobs, j.Job.Id)
		running_jobs.Remove(prev_e)
	}

	reportJobStatus(job_report)
}

func sendHeartbeat() error {
	var hb models.WorkerHeartbeat
	hb.Worker_id = myWorkerId
	hb.LastHeartbeatTime = time.Now()
	b, _ := json.Marshal(hb)

	//fmt.Println("Sending heartbeat at time =", hb.LastHeartbeatTime)
	worker_heartbeat_url := job_scheduler_url + "/" + "heartbeat"
	req, err := http.NewRequest(http.MethodPost, worker_heartbeat_url, bytes.NewReader(b))
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", worker_heartbeat_url)
		// TODO: Need to retry registering new worker instead of giving up
        return errors.New("HeartbeatFailure_fail_to_post")
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        fmt.Println("Failed to POST heartbeat: ", worker_heartbeat_url)
		return err
    }

	// TODO: Need to handle error response (other than http code 200)
	if resp.StatusCode != http.StatusOK {
		fmt.Println("Bad response from scheduler (sendHeartbeat)")
        return errors.New("HeartbeatFailure_fail_to_read_scheduler_response")
	}

	last_confirmed_heartbeat_time = hb.LastHeartbeatTime
	return nil
}

func registerWorker(conf WorkerAppConfig) error {
	register_new_worker_url := job_scheduler_url + "/" + "workers"
	var new_worker_request models.WorkerInfo
	new_worker_request.ServerIp = conf.WorkerAppIp
	new_worker_request.ServerPort = conf.WorkerAppPort
	new_worker_request.CpuCapacity = getCpuCapacity()
	new_worker_request.BandwidthCapacity = getBandwidthCapacity()
	new_worker_request.HeartbeatInterval = scheduler_heartbeat_interval
	
	b, _ := json.Marshal(new_worker_request)

	fmt.Println("Registering new worker at: ", register_new_worker_url, " at time = ", time.Now()) 
	req, err := http.NewRequest(http.MethodPost, register_new_worker_url, bytes.NewReader(b))
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", register_new_worker_url)
        return errors.New("http_post_request_creation_failure")
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", register_new_worker_url)
        return errors.New("http_post_request_send_failure")
    }
	
    defer resp.Body.Close()
    bodyBytes, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Println("Error: Failed to read response body")
        return errors.New("http_post_response_parsing_failure")
    }

	var wkr models.LiveWorker
	json.Unmarshal(bodyBytes, &wkr)
	myWorkerId = wkr.Id
	fmt.Println("Worker registered successfully with worker id =", myWorkerId)
	return nil
}

func main() {
	var logfile, err1 = os.Create("/tmp/worker_app.log")
    if err1 != nil {
        panic(err1)
    }

    configPtr := flag.String("config", "", "config file path")
    flag.Parse()

	if *configPtr != "" {
		worker_app_config_file_path = *configPtr
	}

    Log = log.New(logfile, "", log.LstdFlags)
	readConfig()
	job_scheduler_url = worker_app_config.SchedulerUrl

	// Worker app also acts as a client to the job scheduler when registering itself (worker) and report states/stats
	err1 = registerWorker(worker_app_config)
	if err1 != nil {
		fmt.Println("Failed to register worker. Try again later.")
	}

	available_rtmp_ports = list.New()
	for p := rtmp_port_base; p < rtmp_port_base + max_rtmp_ports; p++ {
		available_rtmp_ports.PushBack(p)
	}

	running_jobs = list.New()

	// Periodic heartbeat
	d, _ := time.ParseDuration(scheduler_heartbeat_interval)
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
		   select {
			case <-ticker.C: {
				// Worker ID is assigned by scheduler only when worker registration succeeded.
				if myWorkerId == "" {
					err1 = registerWorker(worker_app_config)
					if err1 != nil {
						fmt.Println("Failed to register worker. Try again later.")
					}
				} else {
					sendHeartbeat() 
				}
			}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}(ticker)

	// Periodic status check of running jobs
	d2, _ := time.ParseDuration(job_status_check_interval)
	ticker1 := time.NewTicker(d2)
	quit1 := make(chan struct{})
	go func(ticker1 *time.Ticker) {
		for {
		   select {
			case <-ticker1.C: {
				checkJobStatus()
			}
			case <-quit1:
				ticker1.Stop()
				return
			}
		}
	}(ticker1)

	shutdown := make(chan os.Signal, 1)
	// syscall.SIGKILL cannot be handled
    signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
    go func() {
        <-shutdown
        fmt.Println("Shutting down!")

		for e := running_jobs.Front(); e != nil; e = e.Next() {
			j := RunningJob(e.Value.(RunningJob))
			process, err1 := os.FindProcess(int(j.Command.Process.Pid))
			if err1 != nil {
				fmt.Printf("Process id = %d (Job id = %s) not found in stopJob. Error: %v\n", j.Command.Process.Pid, j.Job.Id, err1)
			} else {
				err2 := process.Signal(syscall.Signal(syscall.SIGTERM))
				fmt.Printf("process.Signal.SIGTERM on pid %d (Job id = %s) returned: %v\n", j.Command.Process.Pid, j.Job.Id, err2)
				// The following cleanup steps are not necessarily needed since the whole worker_app 
				// is being shutting down. But let's keep them here.
				if err2 == nil {
					releaseRtmpPort(j.Job.RtmpIngestPort)
					deleteJob(j.Job.Id) 
				}
			}
		}

        os.Exit(0)
    }()

	// Worker app provides web API for handling new job requests received from the job scheduler
	worker_app_addr := worker_app_config.WorkerAppIp + ":" + worker_app_config.WorkerAppPort
	http.HandleFunc("/", main_server_handler)
    fmt.Println("API server listening on: ", worker_app_addr)
    err := http.ListenAndServe(worker_app_addr, nil)
	if err != nil {
		fmt.Println("Server failed to start. Error: ", err)
	}
}