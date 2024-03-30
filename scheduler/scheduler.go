// Job scheduler
package main

import (
	"fmt"
    "os"
	"encoding/json"
	"time"
    //"os/exec"
	"io/ioutil"
	"container/list"
    //"log"
    //"flag"
	"ezliveStreaming/job"
	"ezliveStreaming/job_sqs"
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

var pending_jobs *list.List

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

	 fmt.Println("Job scheduler started...")
	 <-quit
}