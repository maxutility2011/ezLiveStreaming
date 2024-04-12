package models

import (
	"time"
	//"ezliveStreaming/job"
)

const WORKER_STATE_IDLE = "idle"
const WORKER_STATE_LOADED = "loaded"
const WORKER_STATE_NOTAVAILABLE = "notavailable"

// When a worker is in "idle" or "loaded" (but not fully loaded),
// it is eligible for new job assignment.

// When a worker is fully loaded or in a bad state (e.g., missing heartbeats), 
// it is marked as "notavailable"

// When a worker fails to report heartbeats too many times, it is removed from Redis completely.
// However, it can be re-registered later when its heartbeat comes back.

// Worker records maintained by the job scheduler
type LiveWorker struct {
	Id string
	Registered_at time.Time
	State string
	Info WorkerInfo
	//Load WorkerLoad
	LastHeartbeatTime time.Time
}

// Sent by workers when they register with the job scheduler
// Worker ID is assigned by Job scheduler at registration time
type WorkerInfo struct {
	ServerIp string
	ServerPort string
	CpuCapacity string
	BandwidthCapacity string
	HeartbeatInterval string
}

type WorkerJobReport struct {
	WorkerId string
	StoppedJobs []string
}

type JobLoad struct {
	Id string
	CpuLoad int
	BandwidthLoad int
}

// Local table, NOT in Redis
type WorkerLoad struct {
	Id string
	//Jobs map[string]JobLoad
	Jobs []JobLoad
	CpuLoad int
	BandwidthLoad int
}

type WorkerHeartbeat struct {
	Worker_id string
	LastHeartbeatTime time.Time
}