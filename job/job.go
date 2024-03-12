package job

type LiveVideoOutputSpec struct {
	//video_output_label string `json:"label"`
	Codec string 
	Framerate float64
	Width int
	Height int
	Bitrate float64 
	Gop_size int 
}

type LiveAudioOutputSpec struct {
	//audio_output_label string `json:"label"`
	Codec string 
	Bitrate float64 
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
}

type TestJob struct {
	Id string
	Spec LiveJobSpec
}