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

type RedisConfig struct {
	RedisIp string
	RedisPort string
	AllJobs string
	WaitingSet string
	PendingSet string
	DoneSet string
}

type ApiServerConfig struct {
	Sqs SqsConfig
	Redis RedisConfig
}

var liveJobsEndpoint = "jobs"
// TODO: use database to store job states

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
	err := redis.HSetStruct(server_config.Redis.AllJobs, j.Id, j)
	if err != nil {
		fmt.Println("Failed to update job id=", j.Id, ". Error: ", err)
	}

	return err
}

func getJobById(jid string) (job.LiveJob, bool) {
	var j job.LiveJob
	v, e := redis.HGet(server_config.Redis.AllJobs, jid)
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

func getAllJobsByTable(htable string) ([]job.LiveJob, bool) {
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
			fmt.Println("Failed to unmarshal Redis results (getAllJobsByTable). Error: ", e)
			return jobs, false
		}

		jobs = append(jobs, j)
	}

	return jobs, true
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

			var job job.LiveJobSpec
			e := json.NewDecoder(r.Body).Decode(&job)
			if e != nil {
            	res := "Failed to decode job request"
            	fmt.Println("Error happened in JSON marshal. Err: %s", e)
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

			// Send the new job to job scheduler via SQS
			jobMsg := string(b[:])
			e2 = sqs_sender.SendMsg(jobMsg, j.Id)
			if e2 != nil {
				fmt.Println("Failed to send SQS message. Error: ", e2)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			FileContentType := "application/json"
        	w.Header().Set("Content-Type", FileContentType)
        	w.WriteHeader(http.StatusCreated)
        	json.NewEncoder(w).Encode(j)

			/*
			var workerArgs []string
			paramArg := "-param="
			paramArg += string(b[:])
			workerArgs = append(workerArgs, paramArg)

			Log.Println("Worker arguments: ", strings.Join(workerArgs, " "))
			out, err2 := exec.Command("worker", workerArgs...).CombinedOutput()
    		if err2 != nil {
        		log.Fatal("Failed to launch worker: %v ", string(out))
    		}
			*/
		} else if r.Method == "GET" {
			// Get all jobs: /jobs/
			if UrlLastPart == liveJobsEndpoint {
				FileContentType := "application/json"
        		w.Header().Set("Content-Type", FileContentType)
        		w.WriteHeader(http.StatusOK)

				jobs, ok := getAllJobsByTable(server_config.Redis.AllJobs)
				if ok {
					json.NewEncoder(w).Encode(jobs)
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
				} else {
					fmt.Println("Non-existent job id: ", jid)
                    http.Error(w, "Non-existent job id: " + jid, http.StatusNotFound)
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

func readConfig() ApiServerConfig {
	var config ApiServerConfig
	configFile, err := os.Open(server_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close() 
	config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(config_bytes, &config)

	return config
}

func main() {
	var logfile, err1 = os.Create("/tmp/api_server.log")
    if err1 != nil {
        panic(err1)
    }

	server_config = readConfig()
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