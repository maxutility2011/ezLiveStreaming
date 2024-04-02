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
	Gop_size int 
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
	Low_latency_mode bool
	Video_outputs []LiveVideoOutputSpec 
	Audio_outputs []LiveAudioOutputSpec 
}

type LiveJobInputSpec struct {
	Url string 
}

type LiveJobSpec struct {
	Input LiveJobInputSpec
    Output LiveJobOutputSpec 
}

type LiveJob struct {
	Id string
	Spec LiveJobSpec
	StreamKey string
	//IngestUrls []string
	Time_created time.Time
	Time_received_by_scheduler time.Time
	Time_received_by_worker time.Time
}