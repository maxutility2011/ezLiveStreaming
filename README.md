# ezliveStreaming
A work-in-progress live transcoding and streaming service. 

api_server/ contains the implementation of a live streaming api server which accepts requests to create live channels, parse the requests and schedule live workers to fulfill the requests.

job/ contains definition of api requests and live job states.

worker/ contains the implementation of live transcoding/streaming workers.

live_job.json contains a sample live job request.

There are two executables, api_server and worker. After building the project (go build api_server_main.go, go build worker_main.go), start the api_server by "./api_server". The server will run on port 1080 and in its own docker container. I'm still building the container, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_api/general. To submit a live job request, you may use postman to send a POST request to "http://localhost:1080/createLiveJob" (the API endpoint. I'm testing on localhost, but feel free to host the api server anywhere). The request body is the content of live_job.json. The api_server parses the job request to get the live transcoding/packaging parameters, then it will automatically launch a live worker to run the live channel using FFmpeg. The live worker will run also inside a docker container that I'm still building, https://hub.docker.com/repository/docker/maxutility2011/live_streaming_worker/general. 

Currently, all the live job states are saved in memory, but I will move them to a database and/or a distributed data store such as Redis or DynamoDB.

I'm also working on adding other api methods, such as querying, pausing, stopping and deleting a live channel, etc.
