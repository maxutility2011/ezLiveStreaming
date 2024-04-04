package main

import (
	"fmt"
	"errors"
	"os"
	"time"
	"net/http"
	"bytes"
	"strings"
	"log"
	"io/ioutil"
	"encoding/json"
	"ezliveStreaming/models"
	"ezliveStreaming/job"
)

type WorkerAppConfig struct {
	SchedulerUrl string
	WorkerAppIp string
	WorkerAppPort string
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
		if !(r.Method == "POST" || r.Method == "GET") {
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
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			FileContentType := "application/json"
        	w.Header().Set("Content-Type", FileContentType)
        	w.WriteHeader(http.StatusCreated)
        	json.NewEncoder(w).Encode(jobs[jid])
		} else if r.Method == "GET" {
			// Get all jobs: /jobs/
			if UrlLastPart == liveJobsEndpoint {
				FileContentType := "application/json"
        		w.Header().Set("Content-Type", FileContentType)
        		w.WriteHeader(http.StatusOK)
        		json.NewEncoder(w).Encode(jobs)
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
			}
		}
	}
}

func readConfig() WorkerAppConfig {
	var worker_app_config WorkerAppConfig
	configFile, err := os.Open(worker_app_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close() 
	worker_app_config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(worker_app_config_bytes, &worker_app_config)

	return worker_app_config
}

var worker_app_config_file_path = "worker_app_config.json"
var Log *log.Logger
var job_scheduler_url string
var rtmp_port = 1935
var jobs = make(map[string]job.LiveJob) // Live jobs assigned to this worker
var myWorkerId string
var scheduler_heartbeat_interval = "1s" 
var last_confirmed_heartbeat_time time.Time

func getComputeCapacity() string {
	return "5000"
}

func getBandwidthCapacity() string {
	return "100m"
}

func sendHeartbeat() error {
	var hb models.WorkerHeartbeat
	hb.Worker_id = myWorkerId
	hb.LastHeartbeatTime = time.Now()
	b, _ := json.Marshal(hb)

	fmt.Println("Sending heartbeat at time =", hb.LastHeartbeatTime)
	worker_heartbeat_url := job_scheduler_url + "/" + "heartbeat"
	req, err := http.NewRequest(http.MethodPost, worker_heartbeat_url, bytes.NewReader(b))
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", worker_heartbeat_url)
		// TODO: Need to retry registering new worker instead of giving up
        return errors.New("HeartbeatFailure_fail_to_post")
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        panic(err)
    }
	
    defer resp.Body.Close()
    bodyBytes, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Println("Error: Failed to read response body")
        return errors.New("HeartbeatFailure_fail_to_read_scheduler_response")
    }

	// TODO: Need to handle error response (other than http code 200)
	var hb_resp models.WorkerHeartbeat
	json.Unmarshal(bodyBytes, &hb_resp)
	 
	/*if hb_resp.Id == "" {
		fmt.Println("Failed heartbeat. Worker is NOT registered.")
		return errors.New("HeartbeatFailure_unknown_worker")
	}*/

	last_confirmed_heartbeat_time = hb.LastHeartbeatTime
	return nil
}

func main() {
	var logfile, err1 = os.Create("/tmp/worker_app.log")
    if err1 != nil {
        panic(err1)
    }

    Log = log.New(logfile, "", log.LstdFlags)
	conf := readConfig()
	job_scheduler_url = conf.SchedulerUrl

	// Worker app also acts as a client to the job scheduler when registering itself (worker) and report states/stats
	register_new_worker_url := job_scheduler_url + "/" + "workers"
	var new_worker_request models.WorkerInfo
	new_worker_request.ServerIp = conf.WorkerAppIp
	new_worker_request.ServerPort = conf.WorkerAppPort
	new_worker_request.ComputeCapacity = getComputeCapacity()
	new_worker_request.BandwidthCapacity = getBandwidthCapacity()
	new_worker_request.HeartbeatInterval = scheduler_heartbeat_interval
	
	b, _ := json.Marshal(new_worker_request)

	fmt.Println("Registering new worker at: ", register_new_worker_url) 
	req, err := http.NewRequest(http.MethodPost, register_new_worker_url, bytes.NewReader(b))
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", register_new_worker_url)
		// TODO: Need to retry registering new worker instead of giving up
        return
    }
	
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        panic(err)
    }
	
    defer resp.Body.Close()
    bodyBytes, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Println("Error: Failed to read response body")
        return
    }

	var wkr models.LiveWorker
	json.Unmarshal(bodyBytes, &wkr)
	myWorkerId = wkr.Id
	fmt.Println("Worker registered successfully with worker id =", myWorkerId)

	// Periodic heartbeat
	d, _ := time.ParseDuration(scheduler_heartbeat_interval)
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
		   select {
			case <-ticker.C: {
				sendHeartbeat() 
			}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}(ticker)

	// Worker app provides web API for handling new job requests received from the job scheduler
	worker_app_addr := conf.WorkerAppIp + ":" + conf.WorkerAppPort
	http.HandleFunc("/", main_server_handler)
    fmt.Println("API server listening on: ", worker_app_addr)
    http.ListenAndServe(worker_app_addr, nil)
}