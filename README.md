# ezLiveStreaming

ezLiveStreaming is designed to be a highly scalable and efficient live transcoding system written in Go. ezLiveStreaming provides simple API for users to create and manage their live streams via HTTP. A user can create a new live stream by submitting a create_stream request to the API server and specify how she want the live stream to be transcoded and streamed, for example, what the transcoding video/audio codec she want to use, what resolutions/bitrate/frame rate are used for outputting video streams, and what delivery format (Apple-HLS or MPEG-DASH) is used to stream to live viewers. ezLiveStreaming outputs and uploads audio/video segments and manifests/playlists to origin streaming servers for delivering to the viewers. 

ezLiveStreaming is designed to be highly scalable, reliable and resource efficient. This repository also includes a simple transcoding UI for demo purposes. ezLiveStreaming uses **FFmpeg** for live video transcoding and packaging. I'm currently working on using **Shaka packager** to package and DRM-protecting live streams.

## What can ezLiveStreaming do?

- live Adaptative BitRate (ABR) transcoding, 
- HLS/DASH streaming, 
- live transcoding API,
- clear key DRM protection, 
- uploading transcoder outputs to AWS S3 buckets,
- standard-compliant media transcoding and packaging which potentially work with any video players.

# Workflow and high-level architecture

ezLiveStreaming consists of 5 microservices that can be independently scaled,
- live API server
- live job scheduler
- live transcoding worker
- a simple clear-key DRM key server called **ezKey_server**
- **Redis** data store

![screenshot](diagrams/architecture_diagram.png)

The API server exposes API endpoints to users for submitting and managing their live streams. The API server receives job requests from users and sends them to the job scheduler via a job queue (**AWS Simple Queue Service**). The job scheduler then assign the new job requests to live workers from the worker cluster. The live workers launch ffmpeg/shaka packager instances to ingest, transcode, package and output live streams. The users are responsible for generating live input feeds (using for example, RTMP, SRT) and pushing them to the ffmpeg workers. The API server uses a stateless design which the server does not maintain any in-memory states of live jobs. Instead, all the states are kept in Redis data store.

## List of API methods

### Create Job
Creating a new live transcoding request (a.k.a. live transcoding job or live job) <br>
**POST /jobs** <br>
**Request body**: JSON string representing the live job specification <br>
```
{
    "Output": {
        "Stream_type": "hls", 
        "Segment_format": "fmp4", 
        "Fragment_duration": 1, 
        "Segment_duration": 4, 
        "Low_latency_mode": false, 
        "Time_shift_buffer_depth": 120,
        "Drm": {
                "disable_clear_key": false,
                "Protection_system": "FairPlay",
                "Protection_scheme": "cbcs"
            },
        "S3_output": {
            "Bucket": "bzhang-test-bucket-public"
        },
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
                "Threads": 2 
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
                "Threads": 2
            }
        ],
        "Audio_outputs": [
            {
                "Label": "audio128k",
                "Codec": "aac",
                "Bitrate": "128k" 
            }
        ]
    }
}
```
**Response code** on success: 201 created <br>
**Response body**: on success, the server returns the original request body, plus the created job ID, timestamps and job state. <br>

![screenshot](diagrams/create_job.png)

## Transcoding parameter definitions

| Param | Data type | Definition | Valid values |
| --- | --- | --- | --- |
| Stream_type | string | stream type (protocol) | "hls", "dash" |
| Segment_format | string | media segment format | "fmp4", "mpegts", "cmaf" |
| Fragment_duration | integer | fragment (GOP) duration in second. Currently, this will set the closed GOP size and key frame interval | n/a |
| Segment_duration | integer | duration of segments in second | n/a |
| Low_latency_mode | boolean | whether low latency mode is used | n/a |
| Time_shift_buffer_depth | integer | DASH time_shift_buffer_depth in second (applicable to HLS too), i.e., DVR window size | n/a |
| Drm | json | DRM configuration | n/a |
| disable_clear_key | bool | whether clear key DRM is disabled | n/a |
| Protection_system | string | DRM protection system | "FairPlay" (other systems to be added, e.g., "Widewine"m "PlayReady") |
| Protection_scheme | string | DRM protection (encryption) scheme | "cbcs" (other schemes to be added, e.g., "cenc") |
| S3_output | json | output S3 bucket configuration | n/a |
| Bucket | string | S3 bucket name | n/a |
| Video_outputs | json array | Array of video rendition outputs | n/a |
| Label | string | label of an output (rendition) | n/a |
| Codec (Video_outputs) | string | video codec | "h264" (libx264), "h265" (libx265) |
| Framerate | integer | output video frame rate | n/a |
| Width | integer | output video resolution (width) | n/a |
| Height | integer | output video resolution (height) | n/a |
| Bitrate | string | output video bitrate (corresponds to "-b:v" in ffmpeg) | for example, "500k", "1m" |
| Max_bitrate | string | output video bitrate cap (corresponds to "-maxrate" in ffmpeg) | for example, "750k" |
|Buf_size | string | VBV buffer size (corresponds to "-bufsize" in ffmpeg) | for example, "750k" |
| Preset | string | video encoding speed preset (corresponds to "-preset" in ffmpeg) | same as libx264 or libx265 presets |
| Threads | integer | number of encoding threads (corresponds to "-threads" in ffmpeg) | same as ffmpeg "-threads" values |
| Audio_outputs | json | array of audio outputs | n/a |
| Codec (Audio_outputs) | string | audio codec | "aac" |

### Get all the jobs
Show all the jobs including currently running jobs and already finished jobs. <br>
**GET /jobs** <br>
**Request body**: None <br>
**Response code** on success: 200 OK <br>
**Response body**: A JSON array that lists all the jobs. <br>

### Get one job
List a single job given by its ID. <br>
**GET /jobs/[job_id]** <br>
**Request body**: None <br>
**Response code** on success: 200 OK <br>
**Response body**: the requested job <br>

![screenshot](diagrams/get_job.png)

### Stop a job
Stop a job given by its ID. Upon request, the associated worker_transcoder instance will be stopped but the job info and states will remain in Redis. When the job is resumed in the future, the job ID, stream key and all the transcoding and packaging parameters remain the same. <br>

**PUT /jobs/[job_id]** <br>
**Request body**: None <br>
**Response code** on success: 202 Accepted <br>
**Response body**: None <br>

![screenshot](diagrams/stop_job.png)

### Resume a job
Resume a job given by its ID. Upon request, the stopped job will be resumed. A new worker_transcoder instance will be launched. The job ID and stream key and all the transcoding and packaging parameters will be reused. <br>

**PUT /jobs/[job_id]** <br>
**Request body**: None <br>
**Response code** on success: 202 Accepted <br>
**Response body**: None <br>

![screenshot](diagrams/resume_job.png)

The job scheduler periodically polls the job queue and fetches a job from AWS SQS. The frequency of job queue polling must be set low to avoid any delay of job processing. The newly fetched jobs are inserted to the back of the "queued_jobs" list in Redis. On the other side, the job scheduler also periodically checks the front of "queued_jobs" for new jobs. When a new job is found in the "queued_jobs", it is assigned to a transcoding worker from the worker cluster. Different job assignment algorithms can be used, such as random assignment, round robin assignment, etc. The job scheduler is responsible for managing a live job throughout its lifecycle, for examplem, assigning the job to a worker, monitoring its status, restarting/reassigning the job if it fails for any reason. 

The job scheduler also manages a cluster of transcoding workers. Specifically, the scheduler assigns ID to a new worker upon new worker registration, monitor heartbeats of all the workers, monitoring workload of each worker for job assignment purposes, etc. The job scheduler does not maintain any states of live jobs and live workers in memory, rather it keeps all the data in Redis. Therefore, you can put a load balancer in front of all the job scheduler instances, so that any one instance can either serve new jobs from the API server or communicate with the live workers. 

A live transcoding worker (or simply live worker) receives a live job from the job scheduler and launches a worker_transcoder to execute the job. Specifically, the worker launches a ffmpeg transcoder that ingest a live input stream. The input stream can be a RTMP or SRT stream (Currently, ezLiveStreaming only supports RTMP ingest). The worker_transcoder takes the user-specified parameters to transcode the input to multiple outputs with different bitrates. *job/command.go* is responsible for translating the live encoding specification to ffmpeg arguments.

When worker_app on a live worker first starts, it needs to register with the job scheduler and receives a worker id assigned by the scheduler. After that worker_app needs to send periodic heartbeat to the scheduler so that the latter knows the former is still running. If no heartbeat is received for some time from a worker, the scheduler presumes that worker is no longer running and removes it from the active worker set and also remove all the live jobs running on that worker. On the worker side, worker_app needs to periodically ping the live jobs (worker_transcoders) running on it. If a worker_transcoder does not respond, the worker_app presumes that the corresponding live job is no longer running and it will update its workload with the job scheduler. The job scheduler will remove the bad jobs from that worker's workload record.

A list of internal API provided by job scheduler and worker_app can be found in ezLiveStreaming postman_collection.

## DRM configuration

A simple clear key DRM key server is implemented to generate random 16 byte key-id and key upon requests. Each live channel receives an unique key-id and key pair. Specifically, api_server sends a key request to the key server when it receives a new transcoding job with DRM protection configured. The transcoding job ID is used as the content ID for the live channel. The key server generates a random 16 byte key_id and key pair then associate them with the content ID. The api_server receives the key response, parse the key materials from the response, then pass it along with the job request (including the DRM protection configuration) to the scheduler followed by the worker_app and worker_transcoder. The worker_transcode translates the key materials and DRM configuration to Shaka packager DRM options when launching the packager. Lastly, the packager encrypts the live transcoded streams (received from ffmpeg) and outputs DRM-protected HLS/DASH streams. 

```
"Drm": {
    "disable_clear_key": false,
    "Protection_system": "FairPlay",
    "Protection_scheme": "cbcs"
},
```
Currently, ezLiveStreaming **ONLY** supports the above DRM configuration. Particularly we must set *disable_clear_key* to false, *Protection_system* to *FairPlay* and *Protection_scheme* to *cbcs* in order to use clear-key protection scheme. Supporting a full DRM workflow requires integration with 3rd party, paid DRM services which I am happy to implement if sponsorship is provided.

# Code structure

**api_server/** contains the implementation of a live streaming API server which handle requests to create/list/stop/resume live streams.

**demo/** provides the implementation of a simple UI demo. <br>

**drm/** provides the implementation of a simple DRM key server. <br>

**job/** contains the definition of API requests and live job states, and also contains source code for generating FFmpeg (or other encoder library such as GStreamer) commands that are used to execute a live job. <br>

**job_sqs/** contains the implementation of a AWS Simple Queue Service (SQS) sender and receiver. The api_server sends new live jobs to the job queue (AWS SQS). The job scheduler periodically polls the job queue to receive new jobs. <br>

**model/** contains various model definitions. <br>

**redis_client/** implements a redis client wrapper based on go_redis (https://github.com/redis/go-redis). <br>

**scheduler/** contains the implementation of a live job scheduler. Job scheduler receives new live jobs from the api_server via a AWS SQS job queue. Job scheduler also exposes API endpoints and receives new live worker registration requests from newly launched workers. <br>

**worker/** contains the implementation of live transcoding/streaming workers. The file *app/worker_app.go* implements the main application of the live worker. There is only one worker_app running on each live worker. worker_app receives live transcoding jobs from the job scheduler, launch new worker_transcoder (*worker/transcoder/worker_transcode.go*) to process live inputs and generate outputs, sends hearbeat periodically to the job scheduler, reports status of jobs and current workload to the job scheduler, etc. *worker/* also contains the Shaka packager binary "packager" (the nightly build from 04/2024).
<br>

**sample_live_job.json** contains a sample live job request. <br>

**ezLiveStreaming.postman_collection.json** provides sample API requests to ezLiveStreaming in a postman collection.

There are four executables, **api_server**, **job scheduler** and **worker_app**, **worker_transcoder**. The entire live transcoding system consists of a cluster of api_server(s), a cluster of job schedulers, a cluster of redis servers and a cluster of live workers. Neither an api_server nor a job scheduler maintains any states of the live transcoding requests. The stateless design allows easy scalability and failover. As a result, one can put a load balancer (such as Nginx) in front of the api_server cluster and the job scheduler cluster. For example, you can use the "*upstream*" directive (https://docs.nginx.com/nginx/admin-guide/load-balancer/tcp-udp-load-balancer/) to specify a cluster of equivalent api_server instances which any one of them can handle the live transcoding requests. The api_server and job scheduler does not communicate directly, rather they communicate via the AWS SQS job queue and Redis. 

On each live worker VM, there runs one instance of worker_app which manages all the live jobs running on the VM. Each live job is executed by one instance of worker_transcoder which coordinates the live transcoder and the live packager to ingest, transcode and package the HLS/DASH live output stream. worker_app is a long-standing daemon while worker_transcoder only lives when a live job is still alive.

# Build and run

To build api_server, go to *api_server/* and run 
```
go build api_server_main.go
```
then start the server by running
```
./api_server_main -config=config.json
``` 
api_server_main does not take any arguments. However, you can set the hostname and network port of the api_server, and the AWS SQS job queue name and Redis server address in *api_server_main/config.json*. By default, the api_server listens for incoming live transcoding requests on http://0.0.0.0:1080/. This is also the base URL of any API endpoints that the server supports.

To build the job scheduler, go to *scheduler/* and run 
```
go build scheduler.go
``` 
then start the job scheduler by running 
```
./scheduler -config=config.json
``` 
Job scheduler does not take any arguments. You can set the hostname and network port of the scheduler, and the AWS SQS job queue name and Redis server address in *scheduler/config.json*. By default, the scheduler listens for incoming requests on http://0.0.0.0:80/.

To build worker_app, go to *worker/app/* and run 
```
go build worker_app.go
``` 
then start the worker by running 
```
./worker_app -config=worker_app_config.json
```
The "*-config*" argument specifies the path to the worker_app configuration file. In the *worker_app_config.json*, you can configure,
- the hostname and network port of the worker_app. 
- the URL of the job scheduler. The worker_app sends heartbeat, reports status of jobs via this URL.
- the IP address or hostname, and network port of the worker VM on which the worker_app runs.

To build ezKey_server, go to *drm/* and run 
```
go build ezKey_server.go
``` 
then start the key server by running
```
./ezKey_server -config=config.json
```

You can write your own docker compose file and/or scripts to automate the deployment of your api_server cluster, the job scheduler cluster, the worker cluster and Redis cluster. I'm also working on providing a sample docker compose file.

You need to configure AWS access to allow the api_server and job scheduler to access AWS SQS - the job queue. Specifically, you need to configure the following environment variables,
```
export AWS_ACCESS_KEY_ID=[your_aws_access_key]
export AWS_SECRET_ACCESS_KEY=[your_aws_secret_key]
export AWS_DEFAULT_REGION=[your_default_aws_region] (optional)
```

Please remember to create your own SQS queue first, and put the queue name in the api_server and scheduler config file.

Additionally, depending on where you install your worker_transcoder and ffmpeg executable, you need to specify the path to the executable by configure the following environment variables,
```
export PATH=[path_to_your_worker_transcoder_binary]:$PATH
export PATH=[path_to_your_ffmpeg_binary]:$PATH
```

You may also configure path to api_server and job scheduler.

Here are a list of docker images that I have created or used for building ezLiveStreaming,
- **ezlivestreaming_server**: https://hub.docker.com/repository/docker/maxutility2011/ezlivestreaming_server  

This is for hosting both the api_server and job scheduler.

- **ezlivestreaming_worker**: https://hub.docker.com/repository/docker/maxutility2011/ezlivestreaming_worker

This is for hosting a single live worker including the worker_app and multiple instances of worker_transcoder.

https://hub.docker.com/_/redis

This is the official Redis image that I'm using.

# List of Redis data structures 
## "jobs": 
All live jobs - see REDIS_KEY_ALLJOBS in redis_client/redis_client.go. <br>
**Data structure**: hash table <br>
**key**: job id <br>
**value**: "type LiveJob struct" in job/job.go <br>

To view all jobs in redis-cli, run "hgetall jobs". <br>

## "queued_jobs": 
Jobs that are pulled from the SQS job queue by job scheduler, but yet to be scheduled - see REDIS_KEY_ALLJOBS in redis_client/redis_client.go. <br>
**Data structure**: list <br>
**value**: string of "type LiveJob struct" (job/job.go) <br>

## "workers":
The set of live workers currently being managed by the job scheduler - see REDIS_KEY_ALLWORKERS in redis_client/redis_client.go. <br>
**Data structure**: hash table <br>
**key**: worker id <br>
**value**: string of "type LiveWorker struct" (models/worker.go) <br>

To view all workers in redis-cli, run "hgetall workers".

## "worker_loads": 
The current load of a worker: list of jobs running on the worker and its CPU and bandwidth load - see REDIS_KEY_WORKER_LOADS in redis_client/redis_client.go.
**Data structure**: hash table
**key**: worker id
**value**: "type LiveWorker struct" in models/worker.go

## "drm_keys":
This table stores all the DRM keys: see REDIS_KEY_DRM_KEYS in redis_client/redis_client.go.
**Data structure**: hash table
**key**: DRM key id
**value**: "type KeyInfo struct" in models/drm.go

# Demo

This repository provide a simple transcoding UI for demo purposes. The demo source code can be found at *demo/* folder,
- demo/demo.html: a simple UI html
- demo/live_demo_player.js: implements listener functions for the "Create", "Stop", "Resume" and "Show", "Livefeed", "Stoplivefeed" and "Play" buttons. Upon button click events, those listener functions will send API requests to the API server. 

Tis demo integrates with the Shaka player (https://github.com/shaka-project/shaka-player) for playing HLS and DASH streams. You don't need to worry about video playback. To set up the demo, you need to first start all the services (api_server, scheduler and at least one worker with worker_app running). You need to run a Nginx web server, https://hub.docker.com/_/nginx to host this demo. You can also use the same Nginx to deliver the HLS/DASH streams. By default, worker_transcoder writes the HLS/DASH streams (media segments and playlists) to /var/www/html/output_[live_job_id] on the docker container which hosts the worker_transcoder, e.g., */var/www/html/output_0e130071-0178-40a1-9a36-2cf80de789a7/*. To avoid cross-origin (CORS) errors, you may need to configure allow-cors in Nginx,
```
location / {
    root   /var/www/html;
    index  index.html index.htm;
    add_header 'Access-Control-Allow-Origin' '*';
}
```

![screenshot](diagrams/demo_step1.png)
First, load the demo UI html in the web browser.

![screenshot](diagrams/demo_step2.png)

Next, click the "Create" button to create a new live stream. The default job request will be used and displayed in the job request editor on the top left corner. You may also edit the default job request.Clicking the "Create" button will send a "create_job" request to api_server. A new live job will be created and a worker_transcoder/ffmpeg instance will be launched. The API response from the api_server will be displayed in the text area on the bottom left corner. The RTMP ingest URL generated by worker_transcoder, and the HLS/DASH stream playback URL generated by the api_server will displayed in the text area on the botton right corner. You can use the RTMP ingest URL as the destination when pushing your live RTMP feed, however by default the demo will automatically push the big_buck_bunny video as a live input to worker_transcoder/ffmpeg. You can also copy the playback URL and play in any HLS/DASH video players, however by default the demo will automatically load the playback URL in Shaka player. The playback will start in about 20 seconds after you click the "Create" button.

![screenshot](diagrams/demo_step3.png)

To stop the demo, you may click the "Stop" button or simply reload the demo page. When you reload the page, the demo program will send a stop_job request to api_server to stop the live stream and stop the worker_transcoder/ffmpeg instance running on the live worker.

