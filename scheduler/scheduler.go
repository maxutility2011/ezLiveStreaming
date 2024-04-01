// Job scheduler
package main

import (
	"fmt"
    "os"
	"net/http"
	"encoding/json"
	"time"
	"strings"
	"bytes"
    //"os/exec"
	"io/ioutil"
	"container/list"
	"math/rand"
	"github.com/google/uuid"
    //"log"
    //"flag"
	"ezliveStreaming/job"
	"ezliveStreaming/job_sqs"
	"ezliveStreaming/models"
)

type SqsConfig struct {
	Queue_name string
}

type SchedulerConfig struct {
	Sqs SqsConfig
}

var scheduler_config_file_path = "config.json"
var sqs_receiver job_sqs.SqsReceiver
var job_scheduling_interval = "0.2s" // Scheduling timer interval
var sqs_poll_interval_multiplier = 2 // Poll SQS job queue every other time when the job scheduling timer fires 

func readConfig() SchedulerConfig {
	var scheduler_config SchedulerConfig
	configFile, err := os.Open(scheduler_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close() 
	scheduler_config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(scheduler_config_bytes, &scheduler_config)

	return scheduler_config
}

func roundRobinAssign(j job.LiveJob) string {
	rd := rand.Intn(len(workers))
	var r string
	var i = 0
	for id, _ := range workers {
		if i == rd {
			r = id
		}

		i++
	}

	fmt.Println("Assign job id=", j.Id, " to worker id=", r)
	return r
}

func assignWorker(j job.LiveJob) string {
	return roundRobinAssign(j)
}

func pollJobQueue(sqs_receiver job_sqs.SqsReceiver) error {
	msgResult, err := sqs_receiver.ReceiveMsg()
	if err != nil {
		fmt.Println(err)
		return err
	}

	//fmt.Println("Scheduler received ", len(msgResult.Messages), " messages at ", time.Now())
	for i := range msgResult.Messages {
		fmt.Println("----------------------------------------")
		fmt.Println("Message ID:     " + *msgResult.Messages[i].MessageId)
		fmt.Println("Message body:     " + *msgResult.Messages[i].Body)
		//fmt.Println("Message receipt handler:     " + *msgResult.Messages[i].ReceiptHandle) 

		var job job.LiveJob
		e := json.Unmarshal([]byte(*msgResult.Messages[i].Body), &job)
		if e != nil {
            fmt.Println("Error happened in JSON marshal. Err: %s", e)
            return e
        }

		job.Time_received_by_scheduler = time.Now()
		pending_jobs.PushBack(job) // https://pkg.go.dev/container/list
		sqs_receiver.DeleteMsg(msgResult.Messages[i].ReceiptHandle)
	}

	return nil
}

func sendJobToWorker(j job.LiveJob, wid string) error {
	worker := workers[wid]
	worker_url := "http://" + worker.Info.ServerIp + ":" + worker.Info.ServerPort + "/" + "jobs"
	
	b, _ := json.Marshal(j)

	fmt.Println("Sending job id=", j.Id, " to worker id=", worker.Id, " at url=", worker_url) 
	req, err := http.NewRequest(http.MethodPost, worker_url, bytes.NewReader(b))
    if err != nil {
        fmt.Println("Error: Failed to POST to: ", worker_url)
		// TODO: Need to retry registering new worker instead of giving up
        return err
    }
	
    resp, err1 := http.DefaultClient.Do(req)
    if err1 != nil {
        return err1
    }
	
    defer resp.Body.Close()
    bodyBytes, err2 := ioutil.ReadAll(resp.Body)
    if err2 != nil {
        fmt.Println("Error: Failed to read response body")
        return err2
    }

	var j2 job.LiveJob
	json.Unmarshal(bodyBytes, &j2)
	j.Timer_received_by_worker = j2.Timer_received_by_worker
	fmt.Println("Job id=", j2.Id, " successfully assigned to worker id=", worker.Id, " at time=", j2.Timer_received_by_worker)
	// TODO: need to update job.Timer_received_by_worker in database

	return nil
}

func scheduleOneJob() {
	e := pending_jobs.Front()
	if e != nil {
		j := job.LiveJob(e.Value.(job.LiveJob))
		pending_jobs.Remove(e)
		fmt.Println("Scheduling job id: ", j.Id)
		assigned_worker_id := assignWorker(j)
		sendJobToWorker(j, assigned_worker_id)
	}
}

func createUpdateWorker(w models.LiveWorker) error {
	workers[w.Id] = w
	return nil
}

func getWorkerById(wid string) (models.LiveWorker, bool) {
	worker, ok := workers[wid]
	return worker, ok
}

func createWorker(wkr models.WorkerInfo) (error, string) {
	var w models.LiveWorker
	w.Id = uuid.New().String()
	fmt.Println("Generating a random worker ID: ", w.Id)

	w.Registered_at = time.Now()
	w.Info = wkr
	w.State = "Ready"

	e := createUpdateWorker(w)
	if e != nil {
		fmt.Println("Error: Failed to create/update worker ID: ", w.Id)
		return e, ""
	}

	w2, ok := getWorkerById(w.Id) 
	if !ok {
		fmt.Println("Error: Failed to find worker ID: ", w.Id)
		return e, ""
	} 

	fmt.Printf("New worker created: %+v\n", w2)
	return nil, w2.Id
}

// TODO: We should NOT save received jobs in memory. They should be saved in a distributed data store.
var pending_jobs *list.List
var server_ip = "0.0.0.0"
var server_port = "80" 
var server_addr = server_ip + ":" + server_port
var workersEndpoint = "workers"
var workers = make(map[string]models.LiveWorker)

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

	if strings.Contains(r.URL.Path, workersEndpoint) {
		if !(r.Method == "POST" || r.Method == "GET")  {
            err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
            fmt.Println(err)
            http.Error(w, "405 method not allowed\n  Error: " + err, http.StatusMethodNotAllowed)
            return
        }

		if r.Method == "POST" && UrlLastPart == workersEndpoint {
			if r.Body == nil {
            	res := "Error: Register worker without worker specification"
            	fmt.Println("Error: Register worker without worker specification")
            	http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            	return
        	}

			var wkr models.WorkerInfo
			e := json.NewDecoder(r.Body).Decode(&wkr)
			if e != nil {
            	res := "Failed to decode worker request"
            	fmt.Println("Error happened in JSON marshal. Err: %s", e)
            	http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            	return
        	}

			e1, wid := createWorker(wkr)
			if e1 != nil {
				fmt.Println("Failed to create new worker. Err: %s", e)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			json.Marshal(workers[wid])

			FileContentType := "application/json"
        	w.Header().Set("Content-Type", FileContentType)
        	w.WriteHeader(http.StatusCreated)
        	json.NewEncoder(w).Encode(workers[wid])
		} else if r.Method == "GET" {
			// Get all workers: /workers/
			if UrlLastPart == workersEndpoint {
				FileContentType := "application/json"
        		w.Header().Set("Content-Type", FileContentType)
        		w.WriteHeader(http.StatusOK)
        		json.NewEncoder(w).Encode(workers)
			} else { // Get one worker: /workers/[worker_id]
				worker, ok := workers[UrlLastPart]
				if ok {
					FileContentType := "application/json"
        			w.Header().Set("Content-Type", FileContentType)
        			w.WriteHeader(http.StatusOK)
        			json.NewEncoder(w).Encode(worker)
				} else {
					fmt.Println("Non-existent worker id: ", UrlLastPart)
                    http.Error(w, "Non-existent worker id: " + UrlLastPart, http.StatusNotFound)
				}
			}
		}
	}
}

func main() {
	conf := readConfig()
	sqs_receiver.QueueName = conf.Sqs.Queue_name
	sqs_receiver.SqsClient = sqs_receiver.CreateClient()

	pending_jobs = list.New()

	d, _ := time.ParseDuration(job_scheduling_interval)
	var timer_counter = 0
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
		   select {
			case <-ticker.C: {
				scheduleOneJob() // Scheduler jobs when timer fires
				timer_counter += 1 
				if timer_counter == sqs_poll_interval_multiplier { // Poll job queue to get new jobs every "sqs_poll_interval_multiplier" times when the timer fires
					timer_counter = 0
					pollJobQueue(sqs_receiver)
				}
			}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}(ticker)

	http.HandleFunc("/", main_server_handler)
    fmt.Println("API server listening on: ", server_addr)
    http.ListenAndServe(server_addr, nil)

	fmt.Println("Job scheduler started...")
	<-quit
}