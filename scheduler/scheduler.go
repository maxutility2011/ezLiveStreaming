// Job scheduler
package main

import (
	"fmt"
    "os"
	"net/http"
	"encoding/json"
	"errors"
	"time"
	"strings"
	"bytes"
	"strconv"
	"io/ioutil"
	"math/rand"
	"github.com/google/uuid"
    //"log"
	"ezliveStreaming/job"
	"ezliveStreaming/job_sqs"
	"ezliveStreaming/models"
	"ezliveStreaming/redis_client"
)

type SqsConfig struct {
	Queue_name string
}

type SchedulerConfig struct {
	Sqs SqsConfig
	Redis redis_client.RedisConfig
}

const workersEndpoint = "workers"
const heartbeatEndpoint = "heartbeat"
const jobStatusEndpoint = "jobstatus"

const scheduler_config_file_path = "config.json"
const update_worker_load_interval = "1s" // update worker load when the previous one failed.  
const job_scheduling_interval = "0.2s" // Scheduling timer interval
const sqs_poll_interval_multiplier = 2 // Poll SQS job queue every other time when the job scheduling timer fires 
const check_worker_heartbeat_interval_multiplier = 25 
const max_missing_heartbeats_before_suspension = 3
const max_missing_heartbeats_before_removal = 10

var sqs_receiver job_sqs.SqsReceiver
var redis redis_client.RedisClient
var scheduler_config SchedulerConfig
//var worker_loads = make(map[string]models.WorkerLoad)

func readConfig() {
	configFile, err := os.Open(scheduler_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close() 
	config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(config_bytes, &scheduler_config)
}

func randomAssign(j job.LiveJob) (string, bool) {
	var r string
	workers, err := getAllAvailableWorkers()
	if err != nil {
		fmt.Println("Failed to getAllAvailableWorkers. Error: ", err)
		return r, false
	}

	num_workers := len(workers)
	if num_workers < 0 {
		fmt.Println("Failed to roundRobinAssign: Invalid num_workers (roundRobinAssign): ", num_workers)
		return "", false
	} else if num_workers == 0 {
		fmt.Println("Failed to roundRobinAssign: No worker available. num_workers=", num_workers)
		return "", false
	}

	fmt.Println("Number of available workers: ", num_workers)
	rn := rand.Intn(num_workers)
	for i, w := range workers {
		if i == rn {
			r = w.Id
		}
	}

	fmt.Println("Assign job id=", j.Id, " to worker id=", r)
	return r, true
}

func assignWorker(j job.LiveJob) (string, bool) {
	wid, ok := randomAssign(j)
	if !ok {
		return "", false
	}

	return wid, true
}

func createUpdateJob(j job.LiveJob) error {
	err := redis.HSetStruct(redis_client.REDIS_KEY_ALLJOBS, j.Id, j)
	if err != nil {
		fmt.Println("Failed to update job id=", j.Id, ". Error: ", err)
	}

	return err
}

// Poll and fetch new jobs from SQS and add to the pending job queue
func pollJobQueue(sqs_receiver job_sqs.SqsReceiver) error {
	msgResult, err := sqs_receiver.ReceiveMsg()
	if err != nil {
		fmt.Println(err)
		return err
	}

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

		// Create_job, stop_job and resume_job share the same job queue.
		// When job.Stop flag is set, the job is to be stopped. Otherwise, it is to be created or resumed.
		if !job.Stop {
			job.Time_received_by_scheduler = time.Now()
			createUpdateJob(job)
		}

		bufferJob(job) 
		sqs_receiver.DeleteMsg(msgResult.Messages[i].ReceiptHandle)
	}

	return nil
}

func estimateJobLoad(j job.LiveJob) (int, int) {
	cpu_load := 1000 
	bandwidth_load := 20000 // kbps
	return cpu_load, bandwidth_load
}

func getWorkerLoadById(wid string) string {
	v, e := redis.HGet(redis_client.REDIS_KEY_WORKER_LOADS, wid)
	var r string
	if e != nil {
		fmt.Println("Warning: Load of worker id=", wid, " NOT found")
	} else {
		r = v
	}

	return r
}

func createUpdateWorkerLoad(wid string, load models.WorkerLoad) error {
	err := redis.HSetStruct(redis_client.REDIS_KEY_WORKER_LOADS, wid, load)
	if err != nil {
		fmt.Println("Failed to add load for worker id = ", wid, ". Error: ", err)
	}

	return err
}

// Function addNewJobLoad() adds new jobs to the table after they are successfully launched on the assigned workers.
// Function updateWorkerStatus() updates load of workers upon reception of worker reports.
// Worker_app periodically check the status of all the running jobs and report any stopped jobs to the scheduler.
// The scheduler updates the "worker_loads" hash table in Redis by subtracting the load of the stopped jobs.
func addNewJobLoad(w models.LiveWorker, j job.LiveJob) error {
	var w_load models.WorkerLoad
	v := getWorkerLoadById(w.Id)
	if v != "" {
		err := json.Unmarshal([]byte(v), &w_load)
		if err != nil {
			fmt.Println("Failed to unmarshal Redis result (addNewJobLoad). Error: ", err)
			return err // Found worker load in Redis but got bad data
		}
	}

	// It is fine if worker load is NOT found since this could be the first job for the worker
	// in which case worker_load is yet to be created for the worker.

/*
	w_load, ok := worker_loads[w.Id]
	// Load of this worker is yet to be created.
	if !ok {
		fmt.Println("Creating new WorkerLoad entry, id = ", w.Id)
		w_load.Jobs = make(map[string]models.JobLoad)
	}
*/

	var j_load models.JobLoad
	j_load.Id = j.Id
	j_load.CpuLoad, j_load.BandwidthLoad = estimateJobLoad(j)
	fmt.Println("Previous Worker Load (worker id = ", w.Id, ") in addNewJobLoad: ")
	fmt.Println("CPU load: ", w_load.CpuLoad)
	fmt.Println("Bandwidth load: ", w_load.BandwidthLoad)

	w_load.Id = w.Id
	w_load.CpuLoad += j_load.CpuLoad
	w_load.BandwidthLoad += j_load.BandwidthLoad
	fmt.Println("New Worker Load (worker id = ", w.Id, ") in addNewJobLoad: ")
	fmt.Println("CPU load: ", w_load.CpuLoad)
	fmt.Println("Bandwidth load: ", w_load.BandwidthLoad)

	//w_load.Jobs[j.Id] = j_load
	//worker_loads[w.Id] = w_load
	w_load.Jobs = append(w_load.Jobs, j_load)
	createUpdateWorkerLoad(w.Id, w_load)

	w.State = models.WORKER_STATE_LOADED
	e := createUpdateWorker(w)
	if e != nil {
		fmt.Println("Failed to update worker load: worker id = ", w.Id, ", job id = ", j.Id, e)
		return e
	}

	return nil
}

func updateWorkerStatus(wid string, a_stopped_job_id string) error {
	/*
	w_load, ok := worker_loads[wid]
	if !ok {
		fmt.Println("Error: Worker id = ", wid, " not found in worker_loads (updateWorkerStatus)")
		return errors.New("WorkerNotFound")
	}
	*/

	var w_load models.WorkerLoad
	v := getWorkerLoadById(wid)
	if v != "" {
		err := json.Unmarshal([]byte(v), &w_load)
		if err != nil {
			fmt.Println("Failed to unmarshal Redis result (addNewJobLoad). Error: ", err)
			return err // Found worker load in Redis but got bad data
		}
	} else {
		fmt.Println("Error: Worker id = ", wid, " not found in worker_loads (updateWorkerStatus)")
		return errors.New("WorkerNotFound")
	}

	/*
	j_load, ok1 := w_load.Jobs[a_stopped_job_id]
	if !ok1 {
		fmt.Println("Job id = ", a_stopped_job_id, " not found in updateWorkerStatus")
		return errors.New("JobNotFound")
	}
	*/

	var j_load models.JobLoad
	j_load_found := false
	var index int
	var j models.JobLoad
	for index, j = range w_load.Jobs {
		if j.Id == a_stopped_job_id {
			j_load = j
			j_load_found = true
			break
		}
	}

	if !j_load_found {
		fmt.Println("Load of job id = ", a_stopped_job_id, " was NOT found assigned to worker id = ", wid)
		return errors.New("JobLoadNotFound")
	}

	fmt.Println("Previous Worker Load (worker id = ", wid, ") in updateWorkerStatus: ")
	fmt.Println("CPU load: ", w_load.CpuLoad)
	fmt.Println("Bandwidth load: ", w_load.BandwidthLoad)

	w_load.CpuLoad -= j_load.CpuLoad
	w_load.BandwidthLoad -= j_load.BandwidthLoad

	fmt.Println("New Worker Load (worker id = ", wid, ") in updateWorkerStatus: ")
	fmt.Println("CPU load: ", w_load.CpuLoad)
	fmt.Println("Bandwidth load: ", w_load.BandwidthLoad)

	// Delete job "a_stopped_job_id" from w_load.Jobs
	w_load.Jobs = append(w_load.Jobs[:index], w_load.Jobs[index+1:]...)

	//delete(w_load.Jobs, a_stopped_job_id)
	//worker_loads[wid] = w_load
	createUpdateWorkerLoad(wid, w_load)
	return nil
}

func sendJobToWorker(j job.LiveJob, wid string) error {
	worker, ok := getWorkerById(wid)
	if !ok {
		fmt.Println("Failed to getWorkerById")
		return errors.New("WorkerNotFound")
	}

	worker_url := "http://" + worker.Info.ServerIp + ":" + worker.Info.ServerPort + "/" + "jobs"
	var req *http.Request
	var err error
	if !(j.Stop || j.Delete) { // create_job or resume_job
		b, _ := json.Marshal(j)
		fmt.Println("Sending job id=", j.Id, " to worker id=", worker.Id, " at url=", worker_url, " at time=", time.Now()) 
		req, err = http.NewRequest(http.MethodPost, worker_url, bytes.NewReader(b))
    	if err != nil {
        	fmt.Println("Error: Failed to POST to: ", worker_url)
			// TODO: Need to retry registering new worker instead of giving up
        	return err
    	}
	} else if j.Stop {
		worker_url += "/"
		worker_url += j.Id
		worker_url += "/stop"
		b, _ := json.Marshal(j)
		fmt.Println("Sending stop_job id=", j.Id, " to worker id=", worker.Id, " at url=", worker_url, " at time=", time.Now()) 
		req, err = http.NewRequest(http.MethodPut, worker_url, bytes.NewReader(b))
    	if err != nil {
        	fmt.Println("Error: Failed to PUT to: ", worker_url)
			// TODO: Need to retry registering new worker instead of giving up
        	return err
    	}
	} //else if j.Delete {
	//}
	
	resp, err1 := http.DefaultClient.Do(req)
    if err1 != nil {
		fmt.Println("Failed to send job request to worker. Error: ", err1)
        return err1
    }
	
	// Case 1: Create_job or resume_job: response status code isn't 202. Return error then retry 
	//         (the job will be put back into "queue_jobs" and be retried later on)
	// Case 2: Stop_job: response status code isn't 200. Return error then retry
	if !(j.Stop || j.Delete) && resp.StatusCode != http.StatusCreated {
		fmt.Println("Job id=", j.Id, " failed to be launched on worker id=", worker.Id, " at time=", j.Time_received_by_worker)
		fmt.Println("Bad worker response status code: ", resp.StatusCode)
		return errors.New("WorkerJobExecutionError")
	} else if (j.Stop && resp.StatusCode != http.StatusOK) {
		fmt.Println("Job id=", j.Id, " failed to be stopped on worker id=", worker.Id)
		fmt.Println("Bad worker response status code: ", resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound {
			return errors.New("jobNotFound")
		} else {
			return errors.New("WorkerJobExecutionError")
		}
	} //else if (j.Delete && resp.StatusCode != http.StatusAccepted) {
	//}

	var e error
	// create_job or resume_job
	if !(j.Stop || j.Delete) { // The assigned worker confirmed the success of job launch. Now, let's update worker load.
    	defer resp.Body.Close()
    	bodyBytes, err2 := ioutil.ReadAll(resp.Body)
    	if err2 != nil {
        	fmt.Println("Error: Failed to read response body. Error: ", err2)
        	return err2
    	}

		var j2 job.LiveJob
		json.Unmarshal(bodyBytes, &j2)
		j2.Assigned_worker_id = wid
		j2.State = job.JOB_STATE_RUNNING
		// job.RtmpIngestUrl is set by and returned from worker_app
		createUpdateJob(j2)
		e = addNewJobLoad(worker, j2)

		// Do we need to keep retrying addNewJobLoad?
		/*
		if e != nil {
			d, _ := time.ParseDuration(update_worker_load_interval)
			ticker := time.NewTicker(d)
			quit := make(chan bool)
			go func(ticker *time.Ticker) {
				for {
			   		select {
						case <-ticker.C: {
							e = addNewJobLoad(worker, j2) 
							fmt.Println("Retrying worker load update...")
							if e == nil {
								fmt.Println("Worker load update retried and succeeded!")
								quit <- true
							}
						}
						case <-quit:
							ticker.Stop()
							return
						}
					}
				}(ticker)
			}
		}
		*/

		fmt.Println("Job id=", j2.Id, " is successfully launched on worker id = ", wid, " at time = ", j2.Time_received_by_worker)
	} else if j.Stop { // The assigned worker confirmed the success of job stop, there is nothing scheduler needs to do at this moment. Worker load will be updated upon the next worker report when the worker_transcoder process (of this job) is terminated.
		j.State = job.JOB_STATE_STOPPED
		j.Assigned_worker_id = "" // A different worker will be assigned when the job is resumed later on
		j.RtmpIngestUrl = "" // RtmpIngestUrl will change when the job is resumed and a new worker is assigned 
		j.Stop = false // Reset the flag
		// j.Id and j.StreamKey will remain the same when the job is resumed
		createUpdateJob(j)
		fmt.Println("Job id = ", j.Id, " is successfully stopped on worker id = ", wid)
	} // else if j.Delete {
	//}

	return e
}

func scheduleOneJob() {
	count := getBufferedJobCount()
	if count <= 0 { // redis error (count < 0) or empty queue (count == 0)
		return
	}

	e, err := getJobBufferFront()
	if err == nil {
		var j job.LiveJob
		err = json.Unmarshal([]byte(e), &j)
		if err != nil {
			fmt.Println("Failed to unmarshal job (scheduleOneJob). Error: ", err)
			return
		}

		popBufferedJob()

		if !(j.Stop || j.Delete) { // create_job or resume_job
			assigned_worker_id, ok := assignWorker(j)
			if !ok {
				fmt.Println("Failed to assign job id=", j.Id, " to a worker")
				bufferJob(j) // Add failed jobs back to the queue and retry later
				return
			}

			err := sendJobToWorker(j, assigned_worker_id)
			if err != nil {
				fmt.Println("Failed to send job to a worker")
				if err.Error() != "jobNotFound" {
					bufferJob(j)
				} 
			}
		} else if j.Stop {
			// When a job was already stopped, j.Assigned_worker_id was cleared. 
			if (j.Assigned_worker_id == "") {
				fmt.Println("Cannot stop Job id = ", j.Id, " because it has no assigned worker. Is it already stopped?")
				return 
			}

			assigned_worker_id := j.Assigned_worker_id
			err := sendJobToWorker(j, assigned_worker_id)
			if err != nil {
				fmt.Println("Failed to send job to a worker")
				bufferJob(j) 
			}
		} // else if j.Delete {
		//}
	}
}

func bufferJob(j job.LiveJob) error {
	err := redis.QPushStruct(redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS, j)
	if err != nil {
		fmt.Println("Failed to buffer job id=", j.Id, ". Error: ", err)
	}

	return err
}

func popBufferedJob() (string, error) {
	j, err := redis.QPop(redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS)
	var r string
	if err != nil {
		fmt.Println("Failed to pop from ", redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS)
	} else {
		r = j
	}

	return r, err
}

func getJobBufferFront() (string, error) {
	j, err := redis.QFront(redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS)
	var r string
	if err != nil {
		fmt.Println("Failed to get front job in ", redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS, ". Error: ", err)
	} else {
		r = j
	}

	return r, err
}

// redis.QLen() returns count = -1 on errors
func getBufferedJobCount() int {
	count, err := redis.QLen(redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS)
	if err != nil {
		fmt.Println("Failed to get buffered job count in ", redis_client.REDIS_KEY_SCHEDULER_QUEUED_JOBS, ". Error: ", err)
	}

	return count
}

func createUpdateWorker(w models.LiveWorker) error {
	err := redis.HSetStruct(redis_client.REDIS_KEY_ALLWORKERS, w.Id, w)
	if err != nil {
		fmt.Println("Failed to update worker id=", w.Id, ". Error: ", err)
	}

	return err
}

func createWorker(wkr models.WorkerInfo) (error, string) {
	var w models.LiveWorker
	w.Id = uuid.New().String()
	fmt.Println("Generating a random worker ID: ", w.Id)

	w.Registered_at = time.Now()
	w.Info = wkr
	w.State = models.WORKER_STATE_IDLE

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

// This function ONLY set job state to "stopped". It does not stop the jobs
func stopWorkerJobs(wid string) error {
	w, err := redis.HGet(redis_client.REDIS_KEY_WORKER_LOADS, wid)
	if err != nil {
		fmt.Println("Failed to get worker id = ", wid, " in Redis (stopWorkerJobs)")
		return err
	}

	var w_load models.WorkerLoad
	err = json.Unmarshal([]byte(w), &w_load)
	if err != nil {
		fmt.Println("Failed to unmarshal load of worker id = ", wid, " in stopWorkerJobs")
		return err
	}

	stopped_jobs_count := 0
	for _, j_load := range w_load.Jobs {
		j, ok := getJobById(j_load.Id)
		if ok {
			j.State = job.JOB_STATE_STOPPED
			createUpdateJob(j)
			stopped_jobs_count++
		} else {
			fmt.Println("Failed to get job id = ", j_load.Id, " in stopWorkerJobs")
		}
	}

	fmt.Println("Stopped ", stopped_jobs_count, " jobs that were running on worker id = ", wid)
	return nil
}

func removeWorker(wid string) (error, string) {
	err := redis.HDelOne(redis_client.REDIS_KEY_ALLWORKERS, wid)
	if err != nil {
		fmt.Println("Failed to delete worker id=", wid, ". Error: ", err)
	}

	return err, wid
}

func getAllWorkerIds() ([]string, error) {
	wids, err := redis.HKeys(redis_client.REDIS_KEY_ALLWORKERS)
	if err != nil {
		fmt.Println("Failed to get all worker IDs. Error: ", err)
	}

	return wids, err
}

func getAllWorkers() ([]string, error) {
	wids, err := redis.HKeys(redis_client.REDIS_KEY_ALLWORKERS)
	if err != nil {
		fmt.Println("Failed to get all worker IDs. Error: ", err)
	}

	var workers []string
	for _, wid := range wids {
		w, err := redis.HGet(redis_client.REDIS_KEY_ALLWORKERS, wid)
		if err != nil {
			return workers, err
		}

		workers = append(workers, w)
	}

	return workers, nil
}

func getAllAvailableWorkers() ([]models.LiveWorker, error) {
	wids, err := redis.HKeys(redis_client.REDIS_KEY_ALLWORKERS)
	if err != nil {
		fmt.Println("Failed to get all worker IDs. Error: ", err)
	}

	var workers []models.LiveWorker
	for _, wid := range wids {
		w, err := redis.HGet(redis_client.REDIS_KEY_ALLWORKERS, wid)
		if err != nil {
			return workers, err
		}

		var worker models.LiveWorker
		err = json.Unmarshal([]byte(w), &worker)
		if err != nil {
			fmt.Println("Failed to unmarshal worker (getAllAvailableWorkers). Error: ", err)
			return workers, err
		}

		if (worker.State == models.WORKER_STATE_IDLE || worker.State == models.WORKER_STATE_LOADED) {
			workers = append(workers, worker)
		}
	}

	return workers, nil
}

func getWorkerById(wid string) (models.LiveWorker, bool) {
	var w models.LiveWorker
	v, e := redis.HGet(redis_client.REDIS_KEY_ALLWORKERS, wid)
	if e != nil {
		fmt.Println("Failed to find worker id=", wid, ". Error: ", e)
		return w, false
	}

	e = json.Unmarshal([]byte(v), &w)
	if e != nil {
		fmt.Println("Failed to unmarshal Redis result (getWorkerById). Error: ", e)
		return w, false
	}

	return w, true
}

func updateNumWorkers(n int) error {
	err := redis.SetKVStruct(redis_client.REDIS_KEY_NUMWORKERS, n, 0)
	if err != nil {
		fmt.Println("Failed to update num_workers. Error: ", err)
	}

	return err
}

func getNumWorkers() int {
	n, err := redis.GetKV(redis_client.REDIS_KEY_NUMWORKERS)
	if err != nil {
		fmt.Println("Failed to get num_workers. Error: ", err)
		return -1
	}

	r, err := strconv.Atoi(n)
	if err != nil {
		fmt.Println("Failed to strconv.Atoi (getNumWorkers). Error: ", err)
		return -1
	}

	return r
}

// Check heartbeat from all the workers 
func check_worker_heartbeat() error {
	workers, err := getAllWorkers()
	if err != nil {
		fmt.Println("Failed to check worker heartbeat. Error: Failed to getAllWorkers: ", err)
		return err
	}

	for _, e := range workers {
		var w models.LiveWorker
		err = json.Unmarshal([]byte(e), &w)
		if err != nil {
			fmt.Println("Failed to check worker heartbeat. Error: Failed to unmarshal workers: ", err)
			return err
		}

		hbinterval, _ := time.ParseDuration(w.Info.HeartbeatInterval)
		time_now := time.Now().UnixMilli()
		time_lastHeartbeat := w.LastHeartbeatTime.UnixMilli()

		//fmt.Println("time elapsed since last heartbeat: ", time_now - time_lastHeartbeat)
		//fmt.Println("max time allowed no heartbeat: ", int64(max_missing_heartbeats_before_suspension * hbinterval / 1000000))
		if (time_lastHeartbeat != 0 && time_now - time_lastHeartbeat > int64(max_missing_heartbeats_before_suspension * hbinterval * 1000)) {
			w.State = models.WORKER_STATE_NOTAVAILABLE
		} 
		
		if (time_lastHeartbeat != 0 && time_now - time_lastHeartbeat > int64(max_missing_heartbeats_before_removal * hbinterval / 1000000)) {
			stopWorkerJobs(w.Id)
			e, wid := removeWorker(w.Id)
			if e != nil {
				// Worker removal failed. Let's try again in the next event. 
				// Meanwhile, let's reassure it is set as "not available" so no job is assigned to it.
				fmt.Println("Failed to remove worker id = ", wid, ". Error: ", err)
				w.State = models.WORKER_STATE_NOTAVAILABLE
			} else {
				fmt.Println("Removed worker id = ", wid, " due to missing heartbeat")
			}
		}
	}

	return nil
}

// TODO: We should NOT save received jobs in memory. They should be saved in a distributed data store.
var server_ip = "0.0.0.0"
var server_port = "80" 
var server_addr = server_ip + ":" + server_port

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

			worker, ok := getWorkerById(wid)
			if !ok {
				fmt.Println("Failed to register worker id=", wid)
				http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
				return
			}

			FileContentType := "application/json"
        	w.Header().Set("Content-Type", FileContentType)
        	w.WriteHeader(http.StatusCreated)
        	json.NewEncoder(w).Encode(worker)
		} else if r.Method == "GET" {
			// Get all workers: /workers/
			if UrlLastPart == workersEndpoint {
				FileContentType := "application/json"
        		w.Header().Set("Content-Type", FileContentType)
        		w.WriteHeader(http.StatusOK)

				workers, err := getAllWorkers()
				if err != nil {
					fmt.Println("Failed to handle request: GET /workers. Error: ", err)
					http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
					return
				}

        		json.NewEncoder(w).Encode(workers)
			} else { // Get one worker: /workers/[worker_id]
				wid := UrlLastPart
				worker, ok := getWorkerById(wid)
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
	} else if strings.Contains(r.URL.Path, heartbeatEndpoint) {
		if !(r.Method == "POST")  {
            err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
            fmt.Println(err)
            http.Error(w, "405 method not allowed\n  Error: " + err, http.StatusMethodNotAllowed)
            return
        }

		if r.Body == nil {
			res := "Error: bad heartbeat received"
			fmt.Println("Error: bad heartbeat received")
			http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
			return
		}

		var hb models.WorkerHeartbeat
		e := json.NewDecoder(r.Body).Decode(&hb)
		if e != nil {
			res := "Failed to decode worker heartbeat"
			fmt.Println("Failed to decode worker heartbeat. Err: %s", e)
			http.Error(w, "400 bad request\n  Error: " + res, http.StatusBadRequest)
			return
		}

		worker, ok := getWorkerById(hb.Worker_id)
		if !ok {
			fmt.Println("Heartbeart worker id =", hb.Worker_id, " does not match any worker in Redis")
			http.Error(w, "500 internal server error\n  Error: ", http.StatusInternalServerError)
			return
		}

		worker.LastHeartbeatTime = hb.LastHeartbeatTime
		createUpdateWorker(worker)
		
		FileContentType := "application/json"
		w.Header().Set("Content-Type", FileContentType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(hb)
	} else if strings.Contains(r.URL.Path, jobStatusEndpoint) {
		if !(r.Method == "POST")  {
            err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
            fmt.Println(err)
            http.Error(w, "405 method not allowed\n Error: " + err, http.StatusMethodNotAllowed)
            return
        }

		if r.Body == nil {
			res := "Error: bad job status report received"
			fmt.Println(res)
			http.Error(w, "400 bad request\n Error: " + res, http.StatusBadRequest)
			return
		}

		var report models.WorkerJobReport
		e := json.NewDecoder(r.Body).Decode(&report)
		if e != nil {
			res := "Failed to decode worker job report"
			fmt.Println("Failed to decode worker job report. Err: %s", e)
			http.Error(w, "400 bad request\n Error: " + res, http.StatusBadRequest)
			return
		}

		for _, j := range report.StoppedJobs {
			e := updateWorkerStatus(report.WorkerId, j)
			if e == nil {
				fmt.Println("Successfully deleted stopped job id = ", j, " and updated load of worker id = ", report.WorkerId)
			} else {
				fmt.Println("Failed to delete stopped job id = ", j, " or failed to update load of worker id = ", report.WorkerId, " Error: ", e)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func main() {
	readConfig()
	sqs_receiver.QueueName = scheduler_config.Sqs.Queue_name
	sqs_receiver.SqsClient = sqs_receiver.CreateClient()

	redis.RedisIp = scheduler_config.Redis.RedisIp
	redis.RedisPort = scheduler_config.Redis.RedisPort
	redis.Client, redis.Ctx = redis.CreateClient(redis.RedisIp, redis.RedisPort)

	d, _ := time.ParseDuration(job_scheduling_interval)
	var sqs_poll_timer_counter = 0
	var worker_heartbeat_timer_counter = 0
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
		   select {
			case <-ticker.C: {
				scheduleOneJob() // Schedule jobs when timer fires
				sqs_poll_timer_counter += 1 
				if sqs_poll_timer_counter == sqs_poll_interval_multiplier { // Poll job queue to get new jobs every "sqs_poll_interval_multiplier" times when the timer fires
					sqs_poll_timer_counter = 0
					pollJobQueue(sqs_receiver)
				}

				worker_heartbeat_timer_counter += 1
				if worker_heartbeat_timer_counter == check_worker_heartbeat_interval_multiplier {
					worker_heartbeat_timer_counter = 0
					check_worker_heartbeat()
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