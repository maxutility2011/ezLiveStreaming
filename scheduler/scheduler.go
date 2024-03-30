// Job scheduler
package main

import (
	"fmt"
    "os"
	"encoding/json"
	"time"
    //"os/exec"
	"io/ioutil"
    //"log"
    //"flag"
	//"ezliveStreaming/job"
	"ezliveStreaming/job_sqs"
)

/*func Schedule() {

}*/

type SqsConfig struct {
	Queue_name string
}

type SchedulerConfig struct {
	Sqs SqsConfig
}

var scheduler_config_file_path = "config.json"
var sqs_receiver job_sqs.SqsReceiver
var sqs_poll_interval = "0.5s" 

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

	fmt.Println("Received ", len(msgResult.Messages), " messages")
	for i := range msgResult.Messages {
		fmt.Println("Message ID:     " + *msgResult.Messages[i].MessageId)
		fmt.Println("Message body:     " + *msgResult.Messages[i].Body)
		fmt.Println("Message receipt handler:     " + *msgResult.Messages[i].ReceiptHandle) 
		sqs_receiver.DeleteMsg(msgResult.Messages[i].ReceiptHandle)
	}

	return nil
}

func main() {
	conf := readConfig()
	sqs_receiver.QueueName = conf.Sqs.Queue_name
	sqs_receiver.SqsClient = sqs_receiver.CreateClient()

	d, _ := time.ParseDuration(sqs_poll_interval)
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
		   select {
			case <-ticker.C:
				pollJobQueue(sqs_receiver)
			case <-quit:
				ticker.Stop()
				return
			}
		}
	 }(ticker)

	 <-quit
}