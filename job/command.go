package job

import (
	"fmt"
    //"os"
	//"net/http"
	"strings"
	"strconv"
	//"encoding/json"
    //"os/exec"
    //"job"
	//"io/ioutil"
    //"log"
    //"flag"
    //"bytes"
)

const RTMP = "rtmp"
const HLS = "hls"
const DASH = "dash"
const H264_CODEC = "h264"
const FFMPEG_H264 = "libx264"
const H265_CODEC = "h265"
const FFMPEG_H265 = "libx265"
const AAC_CODEC = "aac"
const MP3_CODEC = "mp3"
const DEFAULT_MAXBITRATE_AVGBITRATE_RATIO = 1.5

func ArgumentArrayToString(args []string) string {
	return strings.Join(args, " ")
}

// Contribution: ffmpeg -re -i ezliveStreaming/worker/1.mp4 -c copy -f flv rtmp://127.0.0.1:1935/live/app
// Regular latency: ffmpeg -f flv -listen 1 -i rtmp://127.0.0.1:1935/live/app -force_key_frames expr:gte(t,n_forced*4) -map v:0 -s:0 640x360 -c:v libx264 -profile:v baseline -map v:0 -s:1 768x432 -c:v libx264 -profile:v baseline -map a:0 -c:a aac -b:a 128k -seg_duration 4 -window_size 15 -extra_window_size 15 -remove_at_exit 1 -adaptation_sets id=0,streams=v id=1,streams=a -f dash /var/www/html/1.mpd
// Low latency: ffmpeg -f flv -listen 1 -i [live_input] -vf scale=w=640:h=360 -c:v libx264 -profile:v baseline -an -use_template 1 -adaptation_sets "id=0,streams=v id=1,streams=a" -seg_duration 4 -utc_timing_url https://time.akamai.com/?iso -window_size 15 -extra_window_size 15 -remove_at_exit 1 -f dash /var/www/html/[job_ib]/1.mpd
func JobSpecToEncoderArgs(j LiveJobSpec) []string {
    var ffmpegArgs []string 
	
    if strings.Contains(j.Input.Url, RTMP) {
        ffmpegArgs = append(ffmpegArgs, "-f")
        ffmpegArgs = append(ffmpegArgs, "flv")

    	ffmpegArgs = append(ffmpegArgs, "-listen")
    	ffmpegArgs = append(ffmpegArgs, "1")
	}

    ffmpegArgs = append(ffmpegArgs, "-i")
    ffmpegArgs = append(ffmpegArgs, j.Input.Url)

	kf := "expr:gte(t,n_forced*"
	kf += strconv.Itoa(j.Output.Segment_duration)
	kf += ")"

	ffmpegArgs = append(ffmpegArgs, "-force_key_frames")
    ffmpegArgs = append(ffmpegArgs, kf)

	// Video encoding params
	for i := range j.Output.Video_outputs {
		vo := j.Output.Video_outputs[i]

		ffmpegArgs = append(ffmpegArgs, "-map")
		ffmpegArgs = append(ffmpegArgs, "v:0")

		s := "-s:"
		s += strconv.Itoa(i)
		ffmpegArgs = append(ffmpegArgs, s)

		resolution := strconv.Itoa(vo.Width)
		resolution += "x"
		resolution += strconv.Itoa(vo.Height)
		ffmpegArgs = append(ffmpegArgs, resolution)

		ffmpegArgs = append(ffmpegArgs, "-c:v")
		if vo.Codec == H264_CODEC {
			ffmpegArgs = append(ffmpegArgs, FFMPEG_H264)
		} else if vo.Codec == H265_CODEC {
			ffmpegArgs = append(ffmpegArgs, FFMPEG_H265)
		}

		var h26xProfile string
		if vo.Height <= 480 {
			h26xProfile = "baseline"
		} else if vo.Height > 480 && vo.Height <= 720 {
			h26xProfile = "main"
		} else if vo.Height > 720 {
			h26xProfile = "high"
		}

		ffmpegArgs = append(ffmpegArgs, "-profile:v")
		ffmpegArgs = append(ffmpegArgs, h26xProfile)

		if vo.Bitrate != "" && vo.Max_bitrate != "" && vo.Buf_size != "" {
			bv := "-b:v:"
			bv += strconv.Itoa(i)
			ffmpegArgs = append(ffmpegArgs, bv)
			ffmpegArgs = append(ffmpegArgs, vo.Bitrate)

			ffmpegArgs = append(ffmpegArgs, "-maxrate")
			ffmpegArgs = append(ffmpegArgs, vo.Max_bitrate)

			ffmpegArgs = append(ffmpegArgs, "-bufsize")
			ffmpegArgs = append(ffmpegArgs, vo.Buf_size)
		} else if vo.Bitrate != "" && vo.Max_bitrate == "" && vo.Buf_size == "" {
			bv := "-b:v:"
			bv += strconv.Itoa(i)
			ffmpegArgs = append(ffmpegArgs, bv)
			ffmpegArgs = append(ffmpegArgs, vo.Bitrate)
		}

		if vo.Preset != "" {
			ffmpegArgs = append(ffmpegArgs, "-preset")
			ffmpegArgs = append(ffmpegArgs, vo.Preset)
		}

		if vo.Threads != 0 {
			ffmpegArgs = append(ffmpegArgs, "-threads")
			ffmpegArgs = append(ffmpegArgs, strconv.Itoa(vo.Threads))
		}
	}

	// Audio encoding params
	if len(j.Output.Audio_outputs) == 0 {
		ffmpegArgs = append(ffmpegArgs, "-an")
	} else {
		for i := range j.Output.Audio_outputs {
			ao := j.Output.Audio_outputs[i]

			ffmpegArgs = append(ffmpegArgs, "-map")
			ffmpegArgs = append(ffmpegArgs, "a:0")

			ffmpegArgs = append(ffmpegArgs, "-c:a")
			if ao.Codec == AAC_CODEC {
				ffmpegArgs = append(ffmpegArgs, AAC_CODEC)
			} else if ao.Codec == MP3_CODEC {
				ffmpegArgs = append(ffmpegArgs, MP3_CODEC)
			}

			ffmpegArgs = append(ffmpegArgs, "-b:a")
			ffmpegArgs = append(ffmpegArgs, ao.Bitrate)
		}
	} 	

	// Streaming params (HLS, DASH, DRM, etc.)
	ffmpegArgs = append(ffmpegArgs, "-seg_duration")
	ffmpegArgs = append(ffmpegArgs, strconv.Itoa(j.Output.Segment_duration))

	ffmpegArgs = append(ffmpegArgs, "-window_size")
	ffmpegArgs = append(ffmpegArgs, "15")

	ffmpegArgs = append(ffmpegArgs, "-extra_window_size")
	ffmpegArgs = append(ffmpegArgs, "15")

	ffmpegArgs = append(ffmpegArgs, "-remove_at_exit")
	ffmpegArgs = append(ffmpegArgs, "1")

	if j.Output.Low_latency_mode == true {
		ffmpegArgs = append(ffmpegArgs, "-ldash")
		ffmpegArgs = append(ffmpegArgs, "1")

		ffmpegArgs = append(ffmpegArgs, "-streaming")
		ffmpegArgs = append(ffmpegArgs, "1")

		ffmpegArgs = append(ffmpegArgs, "-use_timeline")
		ffmpegArgs = append(ffmpegArgs, "0")

		ffmpegArgs = append(ffmpegArgs, "-use_template")
		ffmpegArgs = append(ffmpegArgs, "1")
	}

	ffmpegArgs = append(ffmpegArgs, "-adaptation_sets")
	ffmpegArgs = append(ffmpegArgs, "id=0,streams=v id=1,streams=a")

	ffmpegArgs = append(ffmpegArgs, "-f")

	if j.Output.Stream_type == DASH {
		ffmpegArgs = append(ffmpegArgs, DASH)
	} else if j.Output.Stream_type == HLS {
		ffmpegArgs = append(ffmpegArgs, HLS)
	}

	ffmpegArgs = append(ffmpegArgs, "/var/www/html/1.mpd")

	ffmpegArgsString := ArgumentArrayToString(ffmpegArgs)
	fmt.Println("FFmpeg arguments: ", ffmpegArgsString)

    return ffmpegArgs
}