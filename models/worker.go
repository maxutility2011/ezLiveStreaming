package models

import (
	"time"
	"ezliveStreaming/job"
)

/*type WorkerJob struct {
	InputUrl string
	Bitrate string
	Video_codec string
	Video_resolution_height string
}*/

// Worker records maintained by the job scheduler
type LiveWorker struct {
	Id string
	Registered_at time.Time
	State string
	Info WorkerInfo
	LastReport WorkerReport
}

// Sent by workers when they register with the job scheduler
// Worker ID is assigned by Job scheduler at registration time
type WorkerInfo struct {
	ServerIp string
	ServerPort string
	ComputeCapacity string
	BandwidthCapacity string
}

// Sent by workers when they report status
type WorkerReport struct {
	Id string
	LiveJobs []job.LiveJob
	ComputeRemaining string
	BandwidthRemaining string
}