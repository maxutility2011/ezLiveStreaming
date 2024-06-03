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
	GetPublicIpUrl string
	WorkerPort string
	WorkerUdpPortBase int
}

type RunningJob struct {
	Job job.LiveJob
	Command *exec.Cmd
	Stats models.LiveJobStats
}

type IPIFY_RESPONSE struct {
	Ip string `json:"ip"`
}

var liveJobsEndpoint = "jobs"
var get_public_ip_url = "https://api.ipify.org?format=json"

func createJob(j job.LiveJob) (error, string) {
	j.Time_received_by_worker = time.Now()
	e := createUpdateJob(j)
	if e != nil {
		Log.Println("Error: Failed to create/update job ID: ", j.Id)
		return e, ""
	}

	j2, ok := getJobById(j.Id) 
	if !ok {
		Log.Println("Error: Failed to find job ID: ", j.Id)
		return e, ""
	} 

	Log.Printf("New job created: %+v\n", j2)
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
		Log.Println("Cannot stop non-existent job id = ", jid)
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
        	Log.Printf("Process id = %d (Job id = %s) not found in stopJob. Error: %v\n", j.Command.Process.Pid, j.Job.Id, err1)
    	} else {
			err2 := process.Signal(syscall.Signal(syscall.SIGTERM))
			Log.Printf("process.Signal.SIGTERM on pid %d (Job id = %s) returned: %v\n", j.Command.Process.Pid, j.Job.Id, err2)
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
		Log.Println("Cannot stop the job. Is job id = ", jid, " running?")
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
    Log.Println("----------------------------------------")
    Log.Println("Received new request:")
    Log.Println(r.Method, r.URL.Path)

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
            Log.Println(err)
            http.Error(w, "405 method not allowed\n  Error: " + err, http.StatusMethodNotAllowed)
            return
        }

		if r.Method == "POST" && UrlLastPart != liveJobsEndpoint {
			res := "POST to " + r.URL.Path + "is not allowed"
			Log.Println(res)
			http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
		} else if r.Method == "POST" && UrlLastPart == liveJobsEndpoint {
			if r.Body == nil {
            	res := "Error New live job without job specification"
            	Log.Println("Error New live job without job specifications")
            	http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            	return
        	}

			var job job.LiveJob
			e := json.NewDecoder(r.Body).Decode(&job)
			if e != nil {
            	res := "Failed to decode job request"
            	Log.Println("Error happened in JSON marshal. Err: %s", e)
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
					Log.Println("Failed to launch job id =", jid, " Error: ", e3)
					deleteJob(jid)
					http.Error(w, "500 Internal serve error\n  Error: ", http.StatusInternalServerError)
					return
				}

				FileContentType := "application/json"
				w.Header().Set("Content-Type", FileContentType)
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(j)
			} else {
				Log.Println("Failed to get job id = ", jid, " (worker_app.main_server_handler)")
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
					Log.Println("Non-existent job id: ", UrlLastPart)
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
					Log.Println("Job id = ", jid, " is successfully stopped")
					w.WriteHeader(http.StatusOK)
				} else if e.Error() == "jobNotFound" {
					http.Error(w, "404 internal server error\n  Error: ", http.StatusNotFound)
				}else {
					Log.Println("Job id = ", jid, " failed to stop")
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
		Log.Println(err)
	}

	defer configFile.Close() 
	config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(config_bytes, &worker_app_config)
}

var rtmp_port_base = 1935 // TODO: Make this configurable
var max_rtmp_ports = 15 // TODO: Make this configurable
var available_rtmp_ports *list.List
var scheduler_heartbeat_interval = "1s" 
var job_status_check_interval = "3s" // not less than 1s
var worker_app_config_file_path = "worker_app_config.json"
var Log *log.Logger
var job_scheduler_url string
// TODO: Need to move "jobs" to Redis or find a way to recover jobs when worker_app crashes
// TODO: Need to constantly monitor job health. Need to re-assign a job to a new worker
//       when the existing worker crashes.
var jobs = make(map[string]job.LiveJob) // Live jobs that have been assigned to this worker since the worker started (including past jobs and running jobs)
var running_jobs *list.List // Live jobs actively running on this worker
var my_worker_id string
var my_public_ip string
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
	Log.Println("Releasing RTMP port: ", port)
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
		//job.RtmpIngestUrl = "rtmp://" + worker_app_config.WorkerHostname + ":" + strconv.Itoa(rtmp_ingest_port) + "/live/" + job.StreamKey
		job.RtmpIngestUrl = "rtmp://" + my_public_ip + ":" + strconv.Itoa(rtmp_ingest_port) + "/live/" + job.StreamKey
		// job.SrtIngestUrl = ...
		// job.RtpIngestUrl = ...
	}

	createUpdateJob(job)
	return err
}

func launchJob(j job.LiveJob) error {
	j.Spec.Input.Url = j.RtmpIngestUrl
	total_outputs := 0
	// Get the total outputs of all the running jobs on this worker and calculate UDP port base for the new job based on
	for e := running_jobs.Front(); e != nil; e = e.Next() {
		rj := RunningJob(e.Value.(RunningJob))
		total_outputs += len(rj.Job.Spec.Output.Video_outputs)
		total_outputs += len(rj.Job.Spec.Output.Audio_outputs)
	}

	// Each new job must be allocated a number of UDP ports, one per each rendition.
	// The ports are used for streaming from FFmpeg transcoder to Shaka packager.
	// WorkerUdpPortBase: port base of the entire worker.
	// total_outputs: total number of output renditions across all the live jobs on this worker.
	// WorkerUdpPortBase + total_outputs: the port base of the new job.
	j.Spec.Input.JobUdpPortBase = worker_app_config.WorkerUdpPortBase + total_outputs
	
	b1, err1 := json.Marshal(j.Spec)
	if err1 != nil {
		Log.Println("Failed to marshal job output (launchJob). Error: ", err1)
		return err1
	}
	
	var b2 []byte
	var err2 error
	if j.DrmEncryptionKeyInfo.Key_id != "" {
		b2, err2 = json.Marshal(j.DrmEncryptionKeyInfo)
		if err2 != nil {
			Log.Println("Failed to marshal job output (launchJob). Error: ", err2)
			return err2
		}
	}

	var transcoderArgs []string

	jobIdArg := "-job_id="
	jobIdArg += j.Id
	transcoderArgs = append(transcoderArgs, jobIdArg)

	paramArg := "-param="
	paramArg += string(b1[:])
	transcoderArgs = append(transcoderArgs, paramArg)

	if j.DrmEncryptionKeyInfo.Key_id != "" {
		drmArg := "-drm="
		drmArg += string(b2[:])
		transcoderArgs = append(transcoderArgs, drmArg)
	}

	Log.Println("Worker transcoder arguments: ", strings.Join(transcoderArgs, " "))
	transcoderCmd := exec.Command("worker_transcoder", transcoderArgs...)

	var rj RunningJob
	rj.Job = j
	rj.Command = transcoderCmd
	//je := running_jobs.PushBack(rj)
	running_jobs.PushBack(rj)

	var out []byte
	var err_transcoder error
	err_transcoder = nil
	go func() {
		out, err_transcoder = transcoderCmd.CombinedOutput() // This line blocks when transcoderCmd launch succeeds
		if err_transcoder != nil {
			//running_jobs.Remove(je) // Cleanup if transcoderCmd fails
			
			// Let's not remove the failed job from running_jobs here, but leave it to function checkJobStatus()
			// checkJobStatus() does more than just removing the job, it also updates worker load with scheduler
        	Log.Println("Errors running worker transcoder: ", string(out))
		}
	}()

	// Wait 100ms to get transcoderCmd (which runs in its own go routine) result (err2). 

	// If we don't wait, err2=nil (its initial value) will always be returned before 
	// transcoderCmd.CombinedOutput() finishes and returns the real result. This will cause inconsistency
	// when launchJob() returns nil (success) but transcoderCmd.CombinedOutput() actually fails. 

	// On the other hand, if transcoderCmd succeeds, the go routine (transcoderCmd.CombinedOutput) blocks until 
	// ffmpeg stops (e.g., when the user issues a stop_job request). In this case, err2 will not be 
	// updated when the sleep ends thus err2=nil will be returned (which is valid). 
	time.Sleep(100 * time.Millisecond)
	if transcoderCmd.Process != nil {
		Log.Println("Worker transcoder process id: ", transcoderCmd.Process.Pid)
	}

	return err_transcoder
}

func reportJobStatus(report models.WorkerJobReport) error {
	//Log.Println("Sending job status report at time =", time.Now())
	job_status_url := job_scheduler_url + "/" + "jobstatus"

	b, _ := json.Marshal(report)
	req, err := http.NewRequest(http.MethodPost, job_status_url, bytes.NewReader(b))
    if err != nil {
        Log.Println("Error: Failed to POST to: ", job_status_url)
		// TODO: Need to retry registering new worker instead of giving up
        return errors.New("StatusReportFailure_fail_to_post")
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        Log.Println("Failed to POST job status: ", job_status_url)
		return err
    }
	
	/*
    defer resp.Body.Close()
    bodyBytes, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        Log.Println("Error: Failed to read response body (reportJobStatus)")
        return errors.New("StatusReportFailure_fail_to_read_scheduler_response")
    }

	var hb_resp models.WorkerHeartbeat
	json.Unmarshal(bodyBytes, &hb_resp)
	*/

	// TODO: Need to handle error response (other than http code 200)
	if resp.StatusCode != http.StatusOK {
		Log.Println("Bad response from scheduler (reportJobStatus)")
        return errors.New("StatusReportFailure_fail_to_read_scheduler_response")
	}

	return nil
}

func readIngressBandwidth(j job.LiveJob) int64 {
	// Use iftop to monitor per-port (per-job) ingress bandwidth
	iftopCmd := exec.Command("sh", "/home/streamer/bins/start_iftop.sh", strconv.Itoa(j.RtmpIngestPort))
	out, err := iftopCmd.CombinedOutput()
	var r int64
	r = 0
	if err != nil {
       	Log.Printf("Errors starting iftop for job id = %s. Error: %v, iftop output: %s", j.Id, err, string(out))
    } else {
		iftop_output := string(out)
		bandwidth_unit := iftop_output[len(iftop_output) - 3 : len(iftop_output) - 1]
		bandwidth_value := iftop_output[: len(iftop_output) - 3]

		bandwidth, err := strconv.ParseFloat(bandwidth_value, 64)
		if err != nil {
			Log.Printf("Invalid bandwidth reading: %s (unit: %s)", bandwidth_value, bandwidth_unit)
			bandwidth = 0
		}

		if bandwidth_unit == "Mb" {
			bandwidth *= 1000
		}

		r = int64(bandwidth)
	}

	Log.Println("Ingress bandwidth: ", r)
	return r
}

func checkJobStatus() {
	Log.Println("Checking job status... Number of running jobs = ", running_jobs.Len())
	var job_report models.WorkerJobReport
	var prev_e *list.Element
	var jobProcessFound bool
	var jobProcessRunning bool

	job_report.WorkerId = my_worker_id
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
			Log.Println("Skip checking partially launched job!")
			prev_e = e
			continue
		}

		process, err1 := os.FindProcess(int(j.Command.Process.Pid))
		if err1 != nil {
            Log.Printf("Process id = %d (Job id = %s) not found. Error: %v\n", j.Command.Process.Pid, j.Job.Id, err1)
			jobProcessFound = false
        } else {
			err2 = process.Signal(syscall.Signal(0))
			Log.Printf("process.Signal 0 on pid %d (Job id = %s) returned: %v\n", j.Command.Process.Pid, j.Job.Id, err2)
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

	// Let's collect stats for the active jobs (after removing all the stopped jobs).
	for e := running_jobs.Front(); e != nil; e = e.Next() {
		j = RunningJob(e.Value.(RunningJob))
		go func() {
			bw := readIngressBandwidth(j.Job)
			lj, ok := getJobById(j.Job.Id) 
			if !ok {
				Log.Println("Error: Failed to find job ID (checkJobStatus): ", j.Job.Id)
			} else {
				lj.Ingress_bandwidth_kbps = bw // cache the bandwidth reading
				createUpdateJob(lj)
			}
		}()

		var stats models.LiveJobStats
		stats.Id = j.Job.Id
		lj, ok := getJobById(j.Job.Id) 
		if !ok {
			Log.Println("Error: Failed to find job ID (checkJobStatus): ", j.Job.Id)
		} else {
			stats.Ingress_bandwidth_kbps = lj.Ingress_bandwidth_kbps // get the cached bandwidth reading from the last stats collection
		}

		job_report.JobStatsReport = append(job_report.JobStatsReport, stats)
	}

	reportJobStatus(job_report)
}

func sendHeartbeat() error {
	var hb models.WorkerHeartbeat
	hb.Worker_id = my_worker_id
	hb.LastHeartbeatTime = time.Now()
	b, _ := json.Marshal(hb)

	//Log.Println("Sending heartbeat at time =", hb.LastHeartbeatTime)
	worker_heartbeat_url := job_scheduler_url + "/" + "heartbeat"
	req, err := http.NewRequest(http.MethodPost, worker_heartbeat_url, bytes.NewReader(b))
    if err != nil {
        Log.Println("Error: Failed to POST to: ", worker_heartbeat_url)
		// TODO: Need to retry registering new worker instead of giving up
        return errors.New("HeartbeatFailure_fail_to_post")
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        Log.Println("Failed to POST heartbeat: ", worker_heartbeat_url)
		return err
    }

	// TODO: Need to handle error response (other than http code 200)
	if resp.StatusCode != http.StatusOK {
		Log.Println("Bad response from scheduler (sendHeartbeat)")
        return errors.New("HeartbeatFailure_fail_to_read_scheduler_response")
	}

	last_confirmed_heartbeat_time = hb.LastHeartbeatTime
	return nil
}

func getPublicIp() (string, error) {
	var public_ip string
	if worker_app_config.GetPublicIpUrl != "" {
		get_public_ip_url = worker_app_config.GetPublicIpUrl
	}
	 
	Log.Println("Getting public IP from: ", get_public_ip_url) 
	req, err1 := http.NewRequest(http.MethodGet, get_public_ip_url, nil)
    if err1 != nil {
        Log.Println("Error: Failed to create GET request to: ", get_public_ip_url)
        return public_ip, errors.New("http_get_request_creation_failure")
    }
	
    resp, err2 := http.DefaultClient.Do(req)
    if err2 != nil {
        Log.Println("Error: Failed to send GET to: ", get_public_ip_url)
        return public_ip, errors.New("http_get_request_send_failure")
    }
	
    defer resp.Body.Close()
    bodyBytes, err3 := ioutil.ReadAll(resp.Body)
    if err3 != nil {
        Log.Println("Error: Failed to read response body from: ", get_public_ip_url)
        return public_ip, errors.New("http_get_response_parsing_failure")
    }

	var ipify IPIFY_RESPONSE
	err := json.Unmarshal(bodyBytes, &ipify)
	if err != nil {
		Log.Println("Failed to parse response from: ", get_public_ip_url)
		return public_ip, err
	}

	public_ip = ipify.Ip
	return public_ip, nil
}

func registerWorker(conf WorkerAppConfig) error {
	register_new_worker_url := job_scheduler_url + "/" + "workers"
	var new_worker_request models.WorkerInfo

	public_ip, err := getPublicIp()
	if err != nil {
		Log.Println("Failed to get public IP from URL: ", get_public_ip_url)
		return err
	}

	new_worker_request.ServerIp = public_ip
	new_worker_request.ServerPort = conf.WorkerPort
	new_worker_request.CpuCapacity = getCpuCapacity()
	new_worker_request.BandwidthCapacity = getBandwidthCapacity()
	new_worker_request.HeartbeatInterval = scheduler_heartbeat_interval
	
	b, _ := json.Marshal(new_worker_request)

	Log.Println("Registering new worker at: ", register_new_worker_url, " at time = ", time.Now()) 
	req, err1 := http.NewRequest(http.MethodPost, register_new_worker_url, bytes.NewReader(b))
    if err1 != nil {
        Log.Println("Error: Failed to POST to: ", register_new_worker_url)
        return errors.New("http_post_request_creation_failure")
    }
	
    resp, err2 := http.DefaultClient.Do(req)
    if err2 != nil {
        Log.Println("Error: Failed to POST to: ", register_new_worker_url)
        return errors.New("http_post_request_send_failure")
    }
	
    defer resp.Body.Close()
    bodyBytes, err3 := ioutil.ReadAll(resp.Body)
    if err3 != nil {
        Log.Println("Error: Failed to read response body")
        return errors.New("http_post_response_parsing_failure")
    }

	var wkr models.LiveWorker
	json.Unmarshal(bodyBytes, &wkr)
	my_worker_id = wkr.Id
	// The public IP of the worker. This is the public IP of the host VM of the worker docker container. 
	// For example, if the worker docker runs on an EC2 VM host, this is the public IP of that VM.
	// We need to know the host VM's public IP for live ingesting.
	my_public_ip = wkr.Info.ServerIp 
	Log.Printf("Worker registered successfully with worker id: %s and my public IP is %s\n", my_worker_id, my_public_ip)
	return nil
}

func main() {
	var logfile, err1 = os.Create("/home/streamer/log/worker_app.log")
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
		Log.Println("Failed to register worker. Try again later.")
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
				if my_worker_id == "" {
					err1 = registerWorker(worker_app_config)
					if err1 != nil {
						Log.Println("Failed to register worker. Try again later.")
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
        Log.Println("Shutting down!")

		for e := running_jobs.Front(); e != nil; e = e.Next() {
			j := RunningJob(e.Value.(RunningJob))
			process, err1 := os.FindProcess(int(j.Command.Process.Pid))
			if err1 != nil {
				Log.Printf("Process id = %d (Job id = %s) not found in stopJob. Error: %v\n", j.Command.Process.Pid, j.Job.Id, err1)
			} else {
				err2 := process.Signal(syscall.Signal(syscall.SIGTERM))
				Log.Printf("process.Signal.SIGTERM on pid %d (Job id = %s) returned: %v\n", j.Command.Process.Pid, j.Job.Id, err2)
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
	worker_app_addr := "0.0.0.0:" + worker_app_config.WorkerPort
	http.HandleFunc("/", main_server_handler)
    fmt.Println("API server listening on: ", worker_app_addr)
    err := http.ListenAndServe(worker_app_addr, nil)
	if err != nil {
		fmt.Println("Server failed to start. Error: ", err)
	}
}