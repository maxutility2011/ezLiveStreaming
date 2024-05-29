package job

import (
	"time"
	"ezliveStreaming/models"
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
	Crf int
	Threads int
}

type LiveAudioOutputSpec struct {
	//audio_output_label string `json:"label"`
	Codec string 
	Bitrate string 
}

type DrmConfig struct {
	disable_clear_key bool
	Protection_system string
	Protection_scheme string
}

type s3OutputConfig struct {
	Bucket string
}

type LiveJobOutputSpec struct {
	Stream_type string 
	Segment_format string 
	Segment_duration int 
	Fragment_duration int
	Low_latency_mode bool
	Time_shift_buffer_depth int
	Drm DrmConfig
	S3_output s3OutputConfig
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
	DrmEncryptionKeyInfo models.KeyInfo
	Stop bool // A flag indicating the job is to be stopped
	Delete bool // A flag indicating the job is to be deleted
}

type CreateLiveJobResponse struct {
	Job LiveJob
	Warnings string
}
