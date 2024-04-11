// The API server for handling live streaming job requests
package main

import (
	"fmt"
	"time"
	"net/http"
	"strings"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"log"
	"io/ioutil"
	"ezliveStreaming/job"
	"ezliveStreaming/job_sqs"
	"ezliveStreaming/redis_client"
)

type SqsConfig struct {
	Queue_name string
}

type ApiServerConfig struct {
	Sqs SqsConfig
	Redis redis_client.RedisConfig
}

var liveJobsEndpoint = "jobs"

func assignJobInputStreamId() string {
	return uuid.New().String()
}

func createJob(j job.LiveJobSpec) (error, job.LiveJob) {
	var lj job.LiveJob
	lj.Id = uuid.New().String()
	
	lj.StreamKey = assignJobInputStreamId()
	//j.IngestUrls = make([]string)
	//RtmpIngestUrl = "rtmp://" + WorkerAppIp + ":" + WorkerAppPort + "/live/" + j.StreamKey
	//j.IngestUrls = append(j.IngestUrls, RtmpIngestUrl)

	lj.Spec = j
	lj.Time_created = time.Now()
	lj.Stop = false // Set to true when the client wants to stop this job
	lj.Delete = false // Set to true when the client wants to delete this job
	lj.State = job.JOB_STATE_CREATED
	fmt.Println("Generating a random job ID: ", lj.Id)

	e := createUpdateJob(lj)
	if e != nil {
		fmt.Println("Error: Failed to create/update job ID: ", lj.Id)
		return e, lj
	}

	j2, ok := getJobById(lj.Id) 
	if !ok {
		fmt.Println("Error: Failed to find job ID: ", lj.Id)
		return e, lj
	} 

	fmt.Printf("New job created: %+v\n", j2)
	return nil, j2
}

func createUpdateJob(j job.LiveJob) error {
	err := redis.HSetStruct(redis_client.REDIS_KEY_ALLJOBS, j.Id, j)
	if err != nil {
		fmt.Println("Failed to update job id=", j.Id, ". Error: ", err)
	}

	return err
}

func stopJob(j job.LiveJob) error {
	j.Stop = true
	err := redis.HSetStruct(redis_client.REDIS_KEY_ALLJOBS, j.Id, j)
	if err != nil {
		fmt.Println("Failed to stop job id=", j.Id, ". Error: ", err)
	}

	return err
}

func resumeJob(j job.LiveJob) error {
	j.Stop = false
	err := redis.HSetStruct(redis_client.REDIS_KEY_ALLJOBS, j.Id, j)
	if err != nil {
		fmt.Println("Failed to stop job id=", j.Id, ". Error: ", err)
	}

	return err
}

func getJobById(jid string) (job.LiveJob, bool) {
	var j job.LiveJob
	v, e := redis.HGet(redis_client.REDIS_KEY_ALLJOBS, jid)
	if e != nil {
		fmt.Println("Failed to find job id=", jid, ". Error: ", e)
		return j, false
	}

	e = json.Unmarshal([]byte(v), &j)
	if e != nil {
		fmt.Println("Failed to unmarshal Redis result (getJobById). Error: ", e)
		return j, false
	}

	return j, true
}

// There are 1 all-jobs table, and 3 sub-tables grouped by job state.
// Please refer to redis_client.go for all the redis key definitions
func getJobsByTable(htable string) ([]job.LiveJob, bool) {
	var jobs []job.LiveJob
	jobsString, e := redis.HGetAll(htable)
	if e != nil {
		fmt.Println("Failed to get all jobs. Error: ", e)
		return jobs, false
	}

	var j job.LiveJob
	for _, j_string := range jobsString {
		e = json.Unmarshal([]byte(j_string), &j)
		if e != nil {
			jobs = nil
			fmt.Println("Failed to unmarshal Redis results (getJobsByTable). Error: ", e)
			return jobs, false
		}

		jobs = append(jobs, j)
	}

	return jobs, true
}

func main_server_handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=ascii")
  	w.Header().Set("Access-Control-Allow-Origin", "*")
  	w.Header().Set("Access-Control-Allow-Headers","Content-Type,access-control-allow-origin, access-control-allow-headers")

	if (*r).Method == "OPTIONS" {
        return
    }

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

			var job job.LiveJobSpec
			e := json.NewDecoder(r.Body).Decode(&job)
			if e != nil {
            	res := "Failed to decode job request"
            	fmt.Println("Error happened in JSON marshal. Err: ", e)
            	http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            	return
        	}

			//Log.Println("Header: ", r.Header)
			//Log.Printf("Job: %+v\n", job)

			e1, j := createJob(job)
			if e1 != nil {
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			b, e2 := json.Marshal(j)
			if e2 != nil {
				fmt.Println("Failed to marshal new job to SQS message. Error: ", e2)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			// Send the create_job to job scheduler via SQS
			// create_job and stop_job share the same job queue.
			// A job "j" with "j.Stop" flag set to true indicates the job is to be stopped.
			// When "j" is added to the job queue and received by scheduler, the latter checks 
			// the "j.Stop" flag to distinguish between a create_job and stop_job.
			jobMsg := string(b[:])
			e2 = sqs_sender.SendMsg(jobMsg, j.Id)
			if e2 != nil {
				fmt.Println("Failed to send SQS message (New job). Error: ", e2)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			FileContentType := "application/json"
        	w.Header().Set("Content-Type", FileContentType)
        	w.WriteHeader(http.StatusCreated)
        	json.NewEncoder(w).Encode(j)
		} else if r.Method == "GET" {
			// Get all jobs: /jobs/
			if UrlLastPart == liveJobsEndpoint {
				FileContentType := "application/json"
        		w.Header().Set("Content-Type", FileContentType)
        		w.WriteHeader(http.StatusOK)

				jobs, ok := getJobsByTable(redis_client.REDIS_KEY_ALLJOBS)
				if ok {
					json.NewEncoder(w).Encode(jobs)
					return
				} else {
					http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
					return
				}
			} else { // Get one job: /jobs/[job_id]
				jid := UrlLastPart
				j, ok := getJobById(jid) 
				if ok {
					FileContentType := "application/json"
        			w.Header().Set("Content-Type", FileContentType)
        			w.WriteHeader(http.StatusOK)
        			json.NewEncoder(w).Encode(j)
					return
				} else {
					fmt.Println("Non-existent job id: ", jid)
                    http.Error(w, "Non-existent job id: " + jid, http.StatusNotFound)
					return
				}
			}
		} else if r.Method == "PUT" {
			if UrlLastPart == liveJobsEndpoint {
				res := "A job id must be provided when updating a job. "
				fmt.Println(res, "Err: ", res)
            	http.Error(w, "403 StatusForbidden\n  Error: " + res, http.StatusForbidden)
            	return
			} else if strings.Contains(r.URL.Path, "stop") {
				begin := strings.Index(r.URL.Path, liveJobsEndpoint) + len(liveJobsEndpoint) + 1
				end := strings.Index(r.URL.Path, "stop") - 1
				jid := r.URL.Path[begin:end]
				fmt.Println(jid)

				j, ok := getJobById(jid) 
				if ok {
					if j.State == job.JOB_STATE_STOPPED {
						res := "Trying to stop a already stopped job id: " + jid
						fmt.Println(res)
						http.Error(w, "403 StatusForbidden\n  Error: " + res, http.StatusForbidden)
						return
					}

					w.WriteHeader(http.StatusAccepted)
        			e1 := stopJob(j) // Update Redis
					j.Stop = true // Set Stop flag to true for the local variable

					if e1 != nil {
						http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
						return
					}
		
					b, e2 := json.Marshal(j)
					if e2 != nil {
						fmt.Println("Failed to marshal stop_job to SQS message. Error: ", e2)
						http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
						return
					}
		
					// Send stop_job to job scheduler via SQS
					jobMsg := string(b[:])
					e2 = sqs_sender.SendMsg(jobMsg, j.Id)
					if e2 != nil {
						fmt.Println("Failed to send SQS message (Stop job). Error: ", e2)
						http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
						return
					}

					return
				} else {
					res := "Trying to stop a non-existent job id: " + jid
					fmt.Println(res)
                    http.Error(w, "403 StatusForbidden\n  Error: " + res, http.StatusForbidden)
					return
				}
			} else if strings.Contains(r.URL.Path, "resume") {
				begin := strings.Index(r.URL.Path, liveJobsEndpoint) + len(liveJobsEndpoint) + 1
				end := strings.Index(r.URL.Path, "resume") - 1
				jid := r.URL.Path[begin:end]
				fmt.Println(jid)

				j, ok := getJobById(jid) 
				if ok {
					if j.State == job.JOB_STATE_RUNNING || j.State == job.JOB_STATE_STREAMING {
						res := "Trying to resume an active job id: " + jid
						fmt.Println(res)
						http.Error(w, "403 StatusForbidden\n  Error: " + res, http.StatusForbidden)
						return
					}

					w.WriteHeader(http.StatusAccepted)
					e1 := resumeJob(j) // Update Redis
					j.Stop = false // Set Stop flag to false for the local variable

					if e1 != nil {
						http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
						return
					}

					b, e2 := json.Marshal(j)
					if e2 != nil {
						fmt.Println("Failed to marshal resume_job to SQS message. Error: ", e2)
						http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
						return
					}
		
					// Send resume_job to job scheduler via SQS
					jobMsg := string(b[:])
					e2 = sqs_sender.SendMsg(jobMsg, j.Id)
					if e2 != nil {
						fmt.Println("Failed to send SQS message (Stop job). Error: ", e2)
						http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
						return
					}

					return
				} else {
					res := "Trying to resume a non-existent job id: " + jid
					fmt.Println(res)
                    http.Error(w, "403 StatusForbidden\n  Error: " + res, http.StatusForbidden)
					return
				}
			}
		}
	}
}

var server_ip = "0.0.0.0"
var server_port = "1080" 
var server_addr = server_ip + ":" + server_port
var Log *log.Logger
var server_config_file_path = "config.json"
var sqs_sender job_sqs.SqsSender
var redis redis_client.RedisClient
var server_config ApiServerConfig

func readConfig() {
	configFile, err := os.Open(server_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close() 
	config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(config_bytes, &server_config)
}

func main() {
	var logfile, err1 = os.Create("/tmp/api_server.log")
    if err1 != nil {
        panic(err1)
    }

	readConfig()
	sqs_sender.QueueName = server_config.Sqs.Queue_name
	sqs_sender.SqsClient = sqs_sender.CreateClient()

	redis.RedisIp = server_config.Redis.RedisIp
	redis.RedisPort = server_config.Redis.RedisPort
	redis.Client, redis.Ctx = redis.CreateClient(redis.RedisIp, redis.RedisPort)

    Log = log.New(logfile, "", log.LstdFlags)
	http.HandleFunc("/", main_server_handler)

    fmt.Println("API server listening on: ", server_addr)
    http.ListenAndServe(server_addr, nil)
}