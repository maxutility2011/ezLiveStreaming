package job

import (
	"fmt"
	"strings"
	"strconv"
)

const RTMP = "rtmp"
const MPEGTS = "mpegts"
const udp_port_base = 10001
const HLS = "hls"
const DASH = "dash"
const H264_CODEC = "h264"
const FFMPEG_H264 = "libx264"
const H265_CODEC = "h265"
const FFMPEG_H265 = "libx265"
const AAC_CODEC = "aac"
const MP3_CODEC = "mp3"
const DEFAULT_MAXBITRATE_AVGBITRATE_RATIO = 1.5
const DASH_MPD_FILE_NAME = "master.mpd"
const HLS_MASTER_PLAYLIST_FILE_NAME = "master.m3u8"
const Media_output_path_prefix = "output_"

func ArgumentArrayToString(args []string) string {
	return strings.Join(args, " ")
}

// ffmpeg -i /tmp/1.mp4 -force_key_frames 'expr:gte(t,n_forced*4)' -map v:0 -s:0 640x360 -c:v libx264 -profile:v baseline -b:v:0 365k -maxrate 500k -bufsize 500k -preset faster -threads 2 -map a:0 -c:a aac -b:a 128k -f mpegts udp://127.0.0.1:10001 -map v:0 -s:1 768x432 -c:v libx264 -profile:v baseline -b:v:1 550k -maxrate 750k -bufsize 750k -preset faster -threads 2 -an -f mpegts udp://127.0.0.1:10002
func JobSpecToFFmpegArgs(j LiveJobSpec, media_output_path string) []string {
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
	kf += strconv.Itoa(j.Output.Fragment_duration) // TODO: need to support sub-second fragment size.
	kf += ")"

	ffmpegArgs = append(ffmpegArgs, "-force_key_frames")
    ffmpegArgs = append(ffmpegArgs, kf)

	port_base := j.Input.JobUdpPortBase

	// ffmpeg generates separate mpegts outputs for audio and video renditions, e.g.,
	// - video rendition 1: udp://127.0.0.1:10001
	// - video rendition 2: udp://127.0.0.1:10002
	// - video rendition 3: udp://127.0.0.1:10003
	// - audio rendition 1: udp://127.0.0.1:10004
	// - audio rendition 2: udp://127.0.0.1:10005

	// ffmpeg and shaka packager must run on the same VM so that we can use localhost (127.0.0.1) address for udp streaming.

	// Video renditions
	var i int
	for i = range j.Output.Video_outputs {
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

		ffmpegArgs = append(ffmpegArgs, "-filter:v")
		fps := "fps="
		fps += strconv.FormatFloat(vo.Framerate, 'f', -1, 64)
		ffmpegArgs = append(ffmpegArgs, fps)

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

		ffmpegArgs = append(ffmpegArgs, "-an") 

		ffmpegArgs = append(ffmpegArgs, "-f")
		ffmpegArgs = append(ffmpegArgs, MPEGTS)
		ffmpegArgs = append(ffmpegArgs, "udp://127.0.0.1:" + strconv.Itoa(port_base + i))
	}

	// Audio renditions
	for k := range j.Output.Audio_outputs {
		ao := j.Output.Audio_outputs[k]

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

		ffmpegArgs = append(ffmpegArgs, "-vn")

		ffmpegArgs = append(ffmpegArgs, "-f")
		ffmpegArgs = append(ffmpegArgs, MPEGTS)
		ffmpegArgs = append(ffmpegArgs, "udp://127.0.0.1:" + strconv.Itoa(port_base + i + 1 + k))
	}

    return ffmpegArgs
}

func JobSpecToShakaPackagerArgs(j LiveJobSpec, media_output_path string) []string {
    var packagerArgs []string 
	port_base := j.Input.JobUdpPortBase

	// In the ffmpeg command, video outputs come first and use the lower UDP ports, starting from the port base. 
	// Audio outputs follow and use the higher UDP ports.
	var i int
	for i = range j.Output.Video_outputs {
		vo := j.Output.Video_outputs[i]

		// ffmpeg and shaka packager must run on the same VM so that we can use localhost (127.0.0.1) address for udp streaming.
		video_output := "in="
		instream := "udp://127.0.0.1:" + strconv.Itoa(port_base + i)
		video_output += instream

		stream_selector := "stream=video"
		video_output += ("," + stream_selector)

		output_label := "video_" + vo.Bitrate

		init_segment_template_prefix := "init_segment="
		init_segment_template := media_output_path + output_label + "/init.mp4"
		video_output += ("," + init_segment_template_prefix + init_segment_template)

		media_segment_template_prefix := "segment_template="
		media_segment_template := media_output_path + output_label + "/seg_$Number$.m4s"
		video_output += ("," + media_segment_template_prefix + media_segment_template)

		if j.Output.Stream_type == HLS {
			playlist_name := "playlist_name=" + media_output_path + output_label + ".m3u8"
			video_output += ("," + playlist_name)
		}

		packagerArgs = append(packagerArgs, video_output)
	}

	for k := range j.Output.Audio_outputs {
		ao := j.Output.Audio_outputs[k]

		audio_output := "in="
		instream := "udp://127.0.0.1:" + strconv.Itoa(port_base + i + 1 + k)
		audio_output += instream

		stream_selector := "stream=audio"
		audio_output += ("," + stream_selector)

		output_label := "audio_" + ao.Bitrate

		init_segment_template_prefix := "init_segment="
		init_segment_template := media_output_path + output_label + "/init.mp4"
		audio_output += ("," + init_segment_template_prefix + init_segment_template)

		media_segment_template_prefix := "segment_template="
		media_segment_template := media_output_path + output_label + "/seg_$Number$.m4s"
		audio_output += ("," + media_segment_template_prefix + media_segment_template)

		if j.Output.Stream_type == HLS {
			playlist_name := "playlist_name=" + media_output_path + output_label + ".m3u8"
			audio_output += ("," + playlist_name)

			hls_group_id := "hls_group_id=" + output_label
			audio_output += ("," + hls_group_id)
		}

		packagerArgs = append(packagerArgs, audio_output)
	}

	if j.Output.Stream_type == DASH {
		frag_duration_option := "--fragment_duration"
		packagerArgs = append(packagerArgs, frag_duration_option)
		frag_duration_value := strconv.Itoa(j.Output.Fragment_duration)
		packagerArgs = append(packagerArgs, frag_duration_value)

		seg_duration_option := "--segment_duration"
		packagerArgs = append(packagerArgs, seg_duration_option)
		seg_duration_value := strconv.Itoa(j.Output.Segment_duration)
		packagerArgs = append(packagerArgs, seg_duration_value)

		min_buffer_time_option := "--min_buffer_time"
		packagerArgs = append(packagerArgs, min_buffer_time_option)
		min_buffer_time_value := "2" // Hardcode min_buffer_time to 2 seconds.
		packagerArgs = append(packagerArgs, min_buffer_time_value)

		minimum_update_period_option := "--minimum_update_period"
		packagerArgs = append(packagerArgs, minimum_update_period_option)
		minimum_update_period_value := "60" // Hardcode minimum_update_period to 60 seconds. TODO: make it configurable
		packagerArgs = append(packagerArgs, minimum_update_period_value)

		time_shift_buffer_depth_option := "--time_shift_buffer_depth"
		packagerArgs = append(packagerArgs, time_shift_buffer_depth_option)
		time_shift_buffer_depth_value := strconv.Itoa(j.Output.Time_shift_buffer_depth) 
		packagerArgs = append(packagerArgs, time_shift_buffer_depth_value)

		preserved_segments_outside_live_window_option := "--preserved_segments_outside_live_window"
		packagerArgs = append(packagerArgs, preserved_segments_outside_live_window_option)
		preserved_segments_outside_live_window_value := "8" // Hardcode to 8 seconds
		packagerArgs = append(packagerArgs, preserved_segments_outside_live_window_value)

		mpd_output := "--mpd_output"
		packagerArgs = append(packagerArgs, mpd_output)
		mpd_output_path := media_output_path + DASH_MPD_FILE_NAME
		packagerArgs = append(packagerArgs, mpd_output_path)
	} else if j.Output.Stream_type == HLS {
		time_shift_buffer_depth_option := "--time_shift_buffer_depth"
		packagerArgs = append(packagerArgs, time_shift_buffer_depth_option)
		time_shift_buffer_depth_value := strconv.Itoa(j.Output.Time_shift_buffer_depth) 
		packagerArgs = append(packagerArgs, time_shift_buffer_depth_value)

		preserved_segments_outside_live_window_option := "--preserved_segments_outside_live_window"
		packagerArgs = append(packagerArgs, preserved_segments_outside_live_window_option)
		preserved_segments_outside_live_window_value := "8" // Hardcode to 8 seconds
		packagerArgs = append(packagerArgs, preserved_segments_outside_live_window_value)

		hls_playlist_type_option := "--hls_playlist_type"
		packagerArgs = append(packagerArgs, hls_playlist_type_option)
		hls_playlist_type_value := "live" 
		packagerArgs = append(packagerArgs, hls_playlist_type_value)

		m3u8_output := "--hls_master_playlist_output"
		packagerArgs = append(packagerArgs, m3u8_output)
		m3u8_output_path := media_output_path + HLS_MASTER_PLAYLIST_FILE_NAME
		packagerArgs = append(packagerArgs, m3u8_output_path)
	}

    return packagerArgs
}

// Contribution: ffmpeg -re -i 1.mp4 -c copy -f flv rtmp://127.0.0.1:1935/live/app
// Regular latency: 
// ffmpeg -f flv -listen 1 -i rtmp://172.17.0.3:1935/live/b1326cd4-9f89-418f-11b-9fe2c19784f5 -force_key_frames 'expr:gte(t,n_forced*4)' -map v:0 -s:0 640x360 -c:v libx264 -profile:v baseline -b:v:0 365k -maxrate 500k -bufsize 500k -preset faster -threads 2 -map v:0 -s:1 768x432 -c:v libx264 -profile:v baseline -b:v:1 550k -maxrate 750k -bufsize 750k -preset faster -threads 2 -map a:0 -c:a aac -b:a 128k -seg_duration 4 -window_size 15 -extra_window_size 15 -remove_at_exit 1 -adaptation_sets 'id=0,streams=v id=1,streams=a' -f dash /var/www/html/1.mpd
// Low latency: ffmpeg -f flv -listen 1 -i rtmp://0.0.0.0:1935/live/app -vf scale=w=640:h=360 -c:v libx264 -profile:v baseline -an -use_template 1 -adaptation_sets "id=0,streams=v id=1,streams=a" -seg_duration 4 -utc_timing_url https://time.akamai.com/?iso -window_size 15 -extra_window_size 15 -remove_at_exit 1 -f dash /var/www/html/[job_ib]/1.mpd
func JobSpecToEncoderArgs(j LiveJobSpec, media_output_path string) []string {
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

	output_path := media_output_path + "1.mpd"
	ffmpegArgs = append(ffmpegArgs, output_path)

	ffmpegArgsString := ArgumentArrayToString(ffmpegArgs)
	fmt.Println("FFmpeg arguments: ", ffmpegArgsString)

    return ffmpegArgs
}
