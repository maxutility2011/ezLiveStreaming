// Job scheduler
package main

import (
	"fmt"
    "os"
	"encoding/json"
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

func main() {
	conf := readConfig()
	sqs_receiver.QueueName = conf.Sqs.Queue_name
	sqs_receiver.SqsClient = sqs_receiver.CreateClient()

	//msgResult := *sqs.ReceiveMessageOutput
	msgResult, err := sqs_receiver.Receive()
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("Message ID:     " + *msgResult.Messages[0].MessageId)
	fmt.Println("Message body:     " + *msgResult.Messages[0].Body)
}