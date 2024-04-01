// Job scheduler
package main

import (
	"fmt"
    "os"
	"net/http"
	"encoding/json"
	"time"
	"strings"
    //"os/exec"
	"io/ioutil"
	"container/list"
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

func pollJobQueue(sqs_receiver job_sqs.SqsReceiver) error {
	msgResult, err := sqs_receiver.ReceiveMsg()
	if err != nil {
		fmt.Println(err)
		return err
	}

	//fmt.Println("Scheduler received ", len(msgResult.Messages), " messages at ", time.Now())
	for i := range msgResult.Messages {
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

func scheduleOneJob() {
	e := pending_jobs.Front()
	if e != nil {
		j := job.LiveJob(e.Value.(job.LiveJob))
		pending_jobs.Remove(e)
		fmt.Println("Scheduling job id: ", j.Id)
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

func createWorker(w models.LiveWorker) (error, string) {
	w.Id = uuid.New().String()
	w.Registered_at = time.Now()
	fmt.Println("Generating a random worker ID: ", w.Id)

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
	return nil, w.Id
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

	if UrlLastPart == workersEndpoint {
		if r.Method != "POST" {
            err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
            fmt.Println(err)
            http.Error(w, "405 method not allowed\n  Error: " + err, http.StatusMethodNotAllowed)
            return
        }

		if r.Body == nil {
            res := "Error: Register worker without worker specification"
            fmt.Println("Error: Register worker without worker specification")
            http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
            return
        }

		var wkr models.LiveWorker
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

		b, _ := json.Marshal(workers[wid])
		fmt.Println("New worker registered:\n", string(b))

		FileContentType := "application/json"
        w.Header().Set("Content-Type", FileContentType)
        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(workers[wid])
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