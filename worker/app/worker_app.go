package main

import (
	"fmt"
	"os"
	//"time"
	"net/http"
	"bytes"
	//"strings"
	"log"
	"io/ioutil"
	"encoding/json"
	"ezliveStreaming/models"
	//"ezliveStreaming/job"
)

type WorkerAppConfig struct {
	SchedulerUrl string
	WorkerAppIp string
	WorkerAppPort string
}

func main_server_handler(w http.ResponseWriter, r *http.Request) {

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
var jobs []models.WorkerJob // Live jobs assigned to this worker
var myId string

func getComputeCapacity() string {
	return "5000"
}
func getBandwidthCapacity() string {
	return "100m"
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
	myId = wkr.Id
	fmt.Println("Assigned worker ID by scheduler: ", myId)

	// Worker app provides web API for handling new job requests received from the job scheduler
	worker_app_addr := conf.WorkerAppIp + ":" + conf.WorkerAppPort
	http.HandleFunc("/", main_server_handler)
    fmt.Println("API server listening on: ", worker_app_addr)
    http.ListenAndServe(worker_app_addr, nil)
}