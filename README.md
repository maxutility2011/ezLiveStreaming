# ezliveStreaming
ezliveStreaming is a highly scalable and efficient live transcoding system written in Go. ezliveStreaming provides simple API methods for users to submit and manage live transcoding requests to the system via HTTP. The requests specify how the user want her live streams to be transcoded and streamed, e.g., the transcoding video/audio codec, output video resolution, bitrate, frame rate and delivery format such as Apple-HLS or MPEG-DASH, etc. ezliveStreaming outputs and uploads audio/video segments and manifests/playlists to the origin streaming server for delivery. ezliveStreaming is designed to be highly scalable, reliable and resource efficient. This repository also includes a simple transcoding UI for demo purposes. ezliveStreaming uses FFmpeg for live video transcoding and packaging.

What can ezliveStreaming do?

Answer: live Adaptative BitRate (ABR) transcoding, HLS/DASH streaming, live transcoding API, DRM protection (to be supported), standard-compliant media transcoding and packaging which potentially work with any video players.

High-level architecture

ezliveStreaming consists of 4 microservices that can be independently scaled,
- An API server
- A live job scheduler
- A cluster of live transcoding worker
- Redis data store

The API server exposes API endpoints to users for submitting and managing live jobs. The API server receives job requests from users and sends them to the job scheduler via a job queue (AWS Simple Queue Service). The API server uses a stateless design which does not maintain any in-memory states of live jobs. Instead, all the states are kept in Redis data store.

The job scheduler fetches a job from AWS SQS and assign it to a transcoding worker from the worker cluster. Different job assignment algorithms can be used, such as random assignment, round robin assignment. The job scheduler is responsible for managing a live job throughout its lifecycle, for examplem, assigning the job, monitoring its status, restarting/reassigning the job if it fails for any reason. The job scheduler also manages a cluster of transcoding workers.

A live transcoding worker receives a live job from the job scheduler and launches a FFmpeg transcoder to carry out the job. Specifically, the worker launches a FFmpeg transcoder that ingest an incoming live input stream. The input stream can be a RTMP or SRT stream (Currently, ezliveStreaming only supports RTMP ingest). The FFmpeg transcoder takes the user-specified parameters to transcode the input to multiple outputs with different bitrates. 

api_server/ contains the implementation of a live streaming api server which accepts requests to create live channels, parse the requests and schedule live workers to fulfill the requests.

job/ contains definition of api requests and live job states, and the FFmpeg (or other encoder library such as GStreamer) commands that are used to execute a live job.

scheduler/ contains the implementation of a live job scheduler. Job scheduler receives new live jobs from the api_server via a AWS SQS job queue. Job scheduler also exposes HTTP endpoints and receives new live worker registration requests from newly launched workers.

job_sqs/ contains the implementation of a AWS Simple Queue Service (SQS) sender and receiver. The api_server sends new live jobs to the job queue (AWS SQS). The job scheduler periodically polls the job queue to receive new jobs.

worker/ contains the implementation of live transcoding/streaming workers.

sample_live_job.json contains a sample live job request.

There are three executables, api_server, job scheduler and worker. After building the project (go build api_server_main.go, go build scheduler.go, go build worker_main.go), start the api_server by "./api_server". The server will run on port 1080 and in its own docker container. I'm still building the container, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_api/general. To submit a live job request, you may use postman to send a POST request to "http://localhost:1080/createLiveJob" (the API endpoint. I'm testing on localhost, but feel free to host the api server anywhere). The request body is the content of live_job.json. The live worker will run also inside a docker container that I'm still building, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_worker/general. 

To run the api_server, run "./api_server". To run the scheduler, run "./scheduler". The live job workers should be launched and managed by the cloud autoscaling algorithm. A newly launched worker should be registered with the job scheduler, then it becomes ready to be assigned new jobs.

Currently, all the live job states are saved in memory, but I plan to move them to a database and/or a distributed data store such as Redis or DynamoDB.

I'm also working on adding other api methods, such as querying, pausing, stopping and deleting a live channel, etc.
