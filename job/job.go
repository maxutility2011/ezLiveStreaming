package job

import (
	"time"
)

type LiveVideoOutputSpec struct {
	//video_output_label string `json:"label"`
	Codec string 
	Framerate float64
	Width int
	Height int
	Bitrate string 
	Max_bitrate string
	Buf_size string
	Preset string
	Threads int
}

type LiveAudioOutputSpec struct {
	//audio_output_label string `json:"label"`
	Codec string 
	Bitrate string 
}

type LiveJobOutputSpec struct {
	Stream_type string 
	Segment_format string 
	Segment_duration int 
	Fragment_duration int
	Low_latency_mode bool
	Time_shift_buffer_depth int
	Video_outputs []LiveVideoOutputSpec 
	Audio_outputs []LiveAudioOutputSpec 
}

type LiveJobInputSpec struct {
	Url string 
	JobUdpPortBase int
}

type LiveJobSpec struct {
	Input LiveJobInputSpec
    Output LiveJobOutputSpec 
}

const JOB_STATE_CREATED = "created" // Created
const JOB_STATE_RUNNING = "running" // worker_transcoder running but not ingesting
const JOB_STATE_STREAMING = "streaming" // Ingesting and transcoding
const JOB_STATE_STOPPED = "stopped" // Stopped

type LiveJob struct {
	Id string
	Spec LiveJobSpec
	StreamKey string
	Playback_url string
	RtmpIngestPort int
	RtmpIngestUrl string
	Time_created time.Time
	Time_received_by_scheduler time.Time
	Time_received_by_worker time.Time
	Assigned_worker_id string
	State string
	Stop bool // A flag indicating the job is to be stopped
	Delete bool // A flag indicating the job is to be deleted
}