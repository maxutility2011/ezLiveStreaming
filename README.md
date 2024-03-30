# ezliveStreaming
A work-in-progress live transcoding and streaming service. 

api_server/ contains the implementation of a live streaming api server which accepts requests to create live channels, parse the requests and schedule live workers to fulfill the requests.

job/ contains definition of api requests and live job states, and the FFmpeg (or other encoder library such as GStreamer) commands that are used to execute a live job.

scheduler/ contains the implementation of a live job scheduler. JOb scheduler receives new live jobs from the api_server.

job_sqs/ contains the implementation of a AWS Simple Queue Service (SQS) sender and receiver. The api_server sends new live jobs to the job queue (AWS SQS). The job scheduler periodically polls the job queue to receive new jobs.

worker/ contains the implementation of live transcoding/streaming workers.

live_job.json contains a sample live job request.

There are three executables, api_server, job scheduler and worker. After building the project (go build api_server_main.go, go build scheduler.go, go build worker_main.go), start the api_server by "./api_server". The server will run on port 1080 and in its own docker container. I'm still building the container, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_api/general. To submit a live job request, you may use postman to send a POST request to "http://localhost:1080/createLiveJob" (the API endpoint. I'm testing on localhost, but feel free to host the api server anywhere). The request body is the content of live_job.json. The live worker will run also inside a docker container that I'm still building, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_worker/general. 

To run the api_server, run "./api_server". To run the scheduler, run "./scheduler". The live job workers should be launched and managed by the cloud autoscaling algorithm. A newly launched worker should be registered with the job scheduler, then it becomes ready to be assigned new jobs.

Currently, all the live job states are saved in memory, but I plan to move them to a database and/or a distributed data store such as Redis or DynamoDB.

I'm also working on adding other api methods, such as querying, pausing, stopping and deleting a live channel, etc.
