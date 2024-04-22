# ezliveStreaming
ezliveStreaming is a highly scalable and efficient live transcoding system written in Go. ezliveStreaming provides simple API for users to submit and manage live transcoding jobs via HTTP. The requests specify how the user want her live streams to be transcoded and streamed, e.g., the transcoding video/audio codec, output video resolution, bitrate, frame rate and delivery format such as Apple-HLS or MPEG-DASH, etc. ezliveStreaming outputs and uploads audio/video segments and manifests/playlists to the origin streaming server for delivery. ezliveStreaming is designed to be highly scalable, reliable and resource efficient. This repository also includes a simple transcoding UI for demo purposes. ezliveStreaming uses **FFmpeg** for live video transcoding and packaging. I'm currently working on using **Shaka packager** to package and DRM-protecting live streams.

# What can ezliveStreaming do?

Answer: 
- live Adaptative BitRate (ABR) transcoding, 
- HLS/DASH streaming, 
- live transcoding API,
- DRM protection (to be supported), 
- standard-compliant media transcoding and packaging which potentially work with any video players.

# High-level architecture

ezliveStreaming consists of 4 microservices that can be independently scaled,
- live API server
- live job scheduler
- live transcoding worker
- **Redis** data store

![screenshot](diagrams/architecture_diagram.png)

The API server exposes API endpoints to users for submitting and managing live transcoding jobs. The API server receives job requests from users and sends them to the job scheduler via a job queue (**AWS Simple Queue Service**). The API server uses a stateless design which the server does not maintain any in-memory states of live jobs. Instead, all the states are kept in Redis data store.

## List of API methods

### Create Job
Creating a new live transcoding request (a.k.a. live transcoding job or live job) <br>
**POST /jobs** <br>
**Request body**: JSON string representing the live job specification <br>
```
{
    "Output": {
        "Stream_type": "dash",
        "Segment_format": "fmp4",
        "Segment_duration": 4,
        "Low_latency_mode": false,
        "Video_outputs": [
            {
                "Label": "video365k",
                "Codec": "h264",
                "Framerate": 25,
                "Width": 640,
                "Height": 360,
                "Bitrate": "365k",
                "Max_bitrate": "500k",
                "Buf_size": "500k",
                "Preset": "faster",
                "Threads": 2,
                "Gop_size": 2
            },
            {
                "Label": "video550k",
                "Codec": "h264",
                "Framerate": 25,
                "Width": 768,
                "Height": 432,
                "Bitrate": "550k",
                "Max_bitrate": "750k",
                "Buf_size": "750k",
                "Preset": "faster",
                "Threads": 2,
                "Gop_size": 2
            }
        ],
        "Audio_outputs": [
            {
                "Codec": "aac",
                "Bitrate": "128k"
            }
        ]
    }
}
```
**Response code** on success: 201 created <br>
**Response body**: on success, the server returns the original request body, plus the created job ID, timestamps and job states. <br>

![screenshot](diagrams/create_job.png)

### Get all the jobs
Show all the jobs including currently running ones and already finished ones. <br>
**GET /jobs** <br>
**Request body**: None <br>
**Response code** on success: 200 OK <br>
**Response body**: A JSON array that lists all the jobs <br>

### Get one job
Show a single job given by its ID. <br>
**GET /jobs/[job_id]** <br>
**Request body**: None <br>
**Response code** on success: 200 OK <br>
**Response body**: the requested job <br>

![screenshot](diagrams/get_job.png)

### Stop a job
Stop a job given by its ID. Upon request, the job states will remain in Redis, but the worker_transcoder and ffmpeg instance will be stopped. The job ID, stream key and all the transcoding and packaging parameters remain the same when the job is resumed in the future. <br>

**PUT /jobs/[job_id]** <br>
**Request body**: None <br>
**Response code** on success: 202 Accepted <br>
**Response body**: None <br>

![screenshot](diagrams/stop_job.png)

### Resume a job
Resume a job given by its ID. Upon request, the stopped job will be resumed. A new worker_transcoder and ffmpeg instance will be launched. The job ID and stream key and all the transcoding and packaging parameters will be reused. <br>

**PUT /jobs/[job_id]** <br>
**Request body**: None <br>
**Response code** on success: 202 Accepted <br>
**Response body**: None <br>

![screenshot](diagrams/resume_job.png)

The job scheduler periodically poll the job queue and fetches a job from AWS SQS and assign it to a transcoding worker from the worker cluster. Different job assignment algorithms can be used, such as random assignment, round robin assignment, etc. The job scheduler is responsible for managing a live job throughout its lifecycle, for examplem, assigning the job to a worker, monitoring its status, restarting/reassigning the job if it fails for any reason. The job scheduler also manages a cluster of transcoding workers.

A live transcoding worker receives a live job from the job scheduler and launches a worker_transcoder to carry out the job. Specifically, the worker launches a ffmpeg transcoder that ingest a live input stream. The input stream can be a RTMP or SRT stream (Currently, ezliveStreaming only supports RTMP ingest). The worker_transcoder takes the user-specified parameters to transcode the input to multiple outputs with different bitrates. 

# Code structure

**api_server/** contains the implementation of a live streaming api server which accepts requests to create live channels, parse the requests and schedule live workers to fulfill the requests.

**demo/** provides source code of a simple UI demo. <br>

**job/** contains definition of api requests and live job states, and the FFmpeg (or other encoder library such as GStreamer) commands that are used to execute a live job. <br>

**job_sqs/** contains the implementation of a AWS Simple Queue Service (SQS) sender and receiver. The api_server sends new live jobs to the job queue (AWS SQS). The job scheduler periodically polls the job queue to receive new jobs. <br>

**model/** contains various model definitions. <br>

**redis_client/** implements a redis client. <br>

**scheduler/** contains the implementation of a live job scheduler. Job scheduler receives new live jobs from the api_server via a AWS SQS job queue. Job scheduler also exposes HTTP endpoints and receives new live worker registration requests from newly launched workers. <br>

**worker/** contains the implementation of live transcoding/streaming workers. The file *app/worker_app.go* implements the main application of the live worker. There is only one worker_app running on each live worker. worker_app receives live transcoding jobs from the job scheduler, launch new worker_transcoder (*worker/transcoder/worker_transcode.go*) to process live inputs and generate outputs, sends hearbeat periodically to the job scheduler, reports status of jobs and current workload to the job scheduler, etc. <br>

**sample_live_job.json** contains a sample live job request. <br>

There are four executables, **api_server**, **job scheduler** and **worker_app**, **worker_transcoder**. The entire live transcoding system may consist of a cluster of api_server, a cluster of job scheduler, a cluster of redis servers and a cluster of live workers. Neither an api_server nor a job scheduler maintains any states of the live transcoding requests. The stateless design allows easy scalability and failover. As a result, one can put a load balancer (such as Nginx) in front of the api_server cluster and the job scheduler cluster. For example, you can use the "*upstream*" directive (https://docs.nginx.com/nginx/admin-guide/load-balancer/tcp-udp-load-balancer/) to specify a cluster of equivalent api_server instances which can handle live transcoding requests. The api_server and job scheduler does not communicate directly, rather they communicate via the job queue and Redis. 

On each live worker VM, there runs one instance of worker_app which manages all the live jobs running on the VM. Each live job is carried out by one instance of worker_transcoder which coordinates the live transcoder and the live packager to ingest, transcode and package the HLS/DASH live stream. worker_app is a long-standing daemon while the lifetime of a worker_transcoder is tied to the lifetime of a live job.

To build api_server, go to *api_server/*  and run "*go build api_server_main.go*", then start the server by running "*./api_server_main*". api_server_main does not take any arguments. However, you can set the hostname and network port of the api_server, and the AWS SQS job queue name and Redis server address in *api_server_main/config.json*. By default, the api_server listens for incoming live transcoding requests on http://0.0.0.0:1080/. This is the base URL of any API endpoints that the server supports.

To build the job scheduler, go to *scheduler/* and run "*go build scheduler.go*", then start the job scheduler by running "*./scheduler*". Job scheduler does not take any arguments. You can set the hostname and network port of the scheduler, and the AWS SQS job queue name and Redis server address in *scheduler/config.json*. By default, the scheduler listens for incoming requests on http://0.0.0.0:80/.

To build worker_app, go to *worker/app/* and run "*go build worker_app.go*", then start the worker by running "*./worker_app -config=worker_app_config.json*". The "*-config*" argument specifies the path to the worker_app configuration file. In the *worker_app_config.json*, you can configure,
- the hostname and network port of the worker_app. 
- the URL of the job scheduler. The worker_app send heartbeat, report status of jobs via this URL.
- the IP address or hostname, and network port of the worker VM that the worker_app runs.

To build your live transcoding system, you can write docker compose file and/or scripts to automate the deployment of your api_server cluster, the job scheduler cluster, the worker cluster and Redis cluster. 

You need to configure AWS access to allow the api_server and job scheduler to access AWS SQS - the job queue. Specifically, you need to configure the following environment variables,
- "export AWS_ACCESS_KEY_ID=[your_aws_access_key]"
- "export AWS_SECRET_ACCESS_KEY=[your_aws_secret_key]"
- "export AWS_DEFAULT_REGION=[your_default_aws_region]" (optional)

Additionally, depending on where you install your worker_transcoder and ffmpeg executable, you need to specify the path to the executable by configure the following environment variables,
- "export PATH=[path_to_your_worker_transcoder_binary]]:$PATH"
- "export PATH=[path_to_your_ffmpeg_binary]]:$PATH"

You may also configure path to api_server and job scheduler.

After building the project (go build api_server_main.go, go build scheduler.go, go build worker_main.go), start the api_server by "*./api_server*". The server will run on port 1080 and in its own docker container. I'm still building the container, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_api/general. To submit a live job request, you may use postman to send a POST request to "http://localhost:1080/jobs" (the API endpoint. I'm testing on localhost, but feel free to host the api server anywhere). The request body is the content of live_job.json. The live worker will run also inside a docker container that I'm still building, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_worker/general. 

To run the api_server, run "*./api_server*". To run the scheduler, run "*./scheduler*". The live job workers should be launched and managed by the cloud autoscaling algorithm. A newly launched worker should be registered with the job scheduler, then it becomes ready to be assigned new jobs.

# List of Redis data structures
## "jobs": 
All live jobs - REDIS_KEY_ALLJOBS in redis_client/redis_client.go
**Data structure**: hash table
**key**: job id
**value**: "type LiveJob struct" in job/job.go

## "queued_jobs": 
Jobs that are pulled from the SQS job queue by job scheduler, but yet to be scheduled - REDIS_KEY_ALLJOBS in redis_client/redis_client.go
**Data structure**: list
**value**: "type LiveJob struct" in job/job.go

## "worker_loads": 
The current load of a worker: list of jobs running on the worker and its CPU and bandwidth load - REDIS_KEY_WORKER_LOADS in redis_client/redis_client.go
**Data structure**: hash table
**key**: worker id
**value**: "type LiveWorker struct" in models/worker.go
