package job

import (
	"strings"
	"strconv"
	"encoding/json"
	"ezliveStreaming/models"
)

const RTMP = "rtmp"
const MPEGTS = "mpegts"
const FMP4 = "fmp4"
const udp_port_base = 10001
const HLS = "hls"
const DASH = "dash"
const H264_CODEC = "h264"
const FFMPEG_H264 = "libx264"
const H265_CODEC = "h265"
const FFMPEG_H265 = "libx265"
const AV1_CODEC = "av1"
const FFMPEG_AV1 = "libsvtav1"
const AAC_CODEC = "aac"
const MP3_CODEC = "mp3"
const PROTECTION_SYSTEM_FAIRPLAY = "FairPlay"
const PROTECTION_SCHEME_CBCS = "cbcs"
const DEFAULT_MAXBITRATE_AVGBITRATE_RATIO = 1.5
const DASH_MPD_FILE_NAME = "master.mpd"
const HLS_MASTER_PLAYLIST_FILE_NAME = "master.m3u8"
const Media_output_path_prefix = "output_"
const drm_label_allmedia = "allmedia"

func ArgumentArrayToString(args []string) string {
	return strings.Join(args, " ")
}

// If any one output rendition uses AV1 video codec, return true;
// Otherwise, return false.
func HasAV1Output(j LiveJobSpec) bool {
	r := false
	for i := range j.Output.Video_outputs {
		vo := j.Output.Video_outputs[i]
        if vo.Codec == AV1_CODEC {
			r = true
			break
		}
    }

	return r
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

    // In the input URL, replace external hostname with anyaddr (0.0.0.0) 
    // The live contribution encoder must use an input URL with external hostname, e.g., rtmp://ec2-34-202-195-77.compute-1.amazonaws.com:1935/live/app.
    // However, FFmpeg transcoder running in docker must listen on anyaddr (0.0.0.0).
    posLastColon := strings.LastIndex(j.Input.Url, ":")
    ffmpegListeningUrl := "rtmp://0.0.0.0:"
    ffmpegListeningUrl = ffmpegListeningUrl + j.Input.Url[posLastColon + 1: ]

    ffmpegArgs = append(ffmpegArgs, "-i")
    ffmpegArgs = append(ffmpegArgs, ffmpegListeningUrl)

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

		if vo.Codec == H264_CODEC || vo.Codec == H265_CODEC {
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
		}

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

		if vo.Crf != "" {
			ffmpegArgs = append(ffmpegArgs, "-crf")
			ffmpegArgs = append(ffmpegArgs, vo.Crf)
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

func JobSpecToShakaPackagerArgs(job_id string, j LiveJobSpec, media_output_path string, drmKeyInfo string) ([]string, []string) {
    var packagerArgs []string 
	var local_media_output_path_subdirs []string
	port_base := j.Input.JobUdpPortBase

	var key models.KeyInfo
	if drmKeyInfo != "" {
        bytesKeyInfoSpec := []byte(drmKeyInfo)
        json.Unmarshal(bytesKeyInfoSpec, &key)
    }

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
		local_media_output_path_subdirs = append(local_media_output_path_subdirs, output_label)

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

		if drmKeyInfo != "" {
			drm_label := "drm_label=" + drm_label_allmedia
			video_output += ("," + drm_label)
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
		local_media_output_path_subdirs = append(local_media_output_path_subdirs, output_label)

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

		if drmKeyInfo != "" {
			drm_label := "drm_label=" + drm_label_allmedia
			audio_output += ("," + drm_label)
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

		// Configure DRM protection 
		if drmKeyInfo != "" {
			/*
			protection_system_option := "--protection_systems"
			packagerArgs = append(packagerArgs, protection_system_option)
			protection_system_value := j.Output.Drm.Protection_system 
			packagerArgs = append(packagerArgs, protection_system_value)
			*/

			protection_scheme_option := "--protection_scheme"
			packagerArgs = append(packagerArgs, protection_scheme_option)
			protection_scheme_value := j.Output.Drm.Protection_scheme 
			packagerArgs = append(packagerArgs, protection_scheme_value)

			// Use clear key
			if !j.Output.Drm.disable_clear_key {
				enable_raw_key_option := "--enable_raw_key_encryption"
				packagerArgs = append(packagerArgs, enable_raw_key_option)

				keys_option := "--keys"
				packagerArgs = append(packagerArgs, keys_option)

				key_label_value := "label=" + drm_label_allmedia 
				key_id_value := "key_id=" + key.Key_id
				key_value := "key=" + key.Key

				iv, _ := models.Random_16bytes_as_string()
				iv_value := "iv=" + iv
				
				keys_value := key_label_value + ":" + key_id_value + ":" + key_value + ":" + iv_value
				packagerArgs = append(packagerArgs, keys_value)

				// Key file URL format: "https://" + j.Output.S3_output.Bucket + ".s3.amazonaws.com/output_" + lj.Id + "/" + models.DrmKeyFileName
				hls_key_uri_option := "--hls_key_uri"
				packagerArgs = append(packagerArgs, hls_key_uri_option)
				hls_key_uri_value := "https://" + j.Output.S3_output.Bucket + ".s3.amazonaws.com/output_" + job_id + "/" + models.DrmKeyFileName
				packagerArgs = append(packagerArgs, hls_key_uri_value)
			}
		}

		m3u8_output := "--hls_master_playlist_output"
		packagerArgs = append(packagerArgs, m3u8_output)
		m3u8_output_path := media_output_path + HLS_MASTER_PLAYLIST_FILE_NAME
		packagerArgs = append(packagerArgs, m3u8_output_path)
	}

    return packagerArgs, local_media_output_path_subdirs
}

// This function is for AV1 ONLY!! 
// Contribution: ffmpeg -re -i 1.mp4 -c copy -f flv rtmp://127.0.0.1:1935/live/app
// Regular latency: 
// ffmpeg -f flv -listen 1 -i rtmp://172.17.0.3:1935/live/b1326cd4-9f89-418f-11b-9fe2c19784f5 -force_key_frames 'expr:gte(t,n_forced*4)' -map v:0 -s:0 640x360 -c:v libx264 -profile:v baseline -b:v:0 365k -maxrate 500k -bufsize 500k -preset faster -threads 2 -map v:0 -s:1 768x432 -c:v libx264 -profile:v baseline -b:v:1 550k -maxrate 750k -bufsize 750k -preset faster -threads 2 -map a:0 -c:a aac -b:a 128k -seg_duration 4 -window_size 15 -extra_window_size 15 -remove_at_exit 1 -adaptation_sets 'id=0,streams=v id=1,streams=a' -f dash /var/www/html/1.mpd
// Low latency: ffmpeg -f flv -listen 1 -i rtmp://0.0.0.0:1935/live/app -vf scale=w=640:h=360 -c:v libx264 -profile:v baseline -an -use_template 1 -adaptation_sets "id=0,streams=v id=1,streams=a" -seg_duration 4 -utc_timing_url https://time.akamai.com/?iso -window_size 15 -extra_window_size 15 -remove_at_exit 1 -f dash /var/www/html/[job_ib]/1.mpd
func JobSpecToEncoderArgs(j LiveJobSpec, media_output_path string) ([]string, []string) {
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

	var local_media_output_path_subdirs []string
	i := 0
	// Video encoding params
	for i = range j.Output.Video_outputs {
		vo := j.Output.Video_outputs[i]
		local_media_output_path_subdirs = append(local_media_output_path_subdirs, "stream_" + strconv.Itoa(i))

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
		if vo.Codec == AV1_CODEC {
			ffmpegArgs = append(ffmpegArgs, FFMPEG_AV1)
		} else {
			return ffmpegArgs, local_media_output_path_subdirs
		}

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

		if vo.Crf != "" {
			ffmpegArgs = append(ffmpegArgs, "-crf")
			ffmpegArgs = append(ffmpegArgs, vo.Crf)
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
		for k := range j.Output.Audio_outputs {
			ao := j.Output.Audio_outputs[k]
			local_media_output_path_subdirs = append(local_media_output_path_subdirs, "stream_" + strconv.Itoa(i+k+1))

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

	if j.Output.Stream_type == DASH {
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
	} else if j.Output.Stream_type == HLS {
		ffmpegArgs = append(ffmpegArgs, "-f")
		ffmpegArgs = append(ffmpegArgs, "hls")

		ffmpegArgs = append(ffmpegArgs, "-hls_playlist_type")
		ffmpegArgs = append(ffmpegArgs, "event")

		ffmpegArgs = append(ffmpegArgs, "-hls_flags")
		ffmpegArgs = append(ffmpegArgs, "independent_segments")

		ffmpegArgs = append(ffmpegArgs, "-hls_segment_type")
		ffmpegArgs = append(ffmpegArgs, j.Output.Segment_format)

		ffmpegArgs = append(ffmpegArgs, "-hls_time")
		ffmpegArgs = append(ffmpegArgs, strconv.Itoa(j.Output.Segment_duration))

		ffmpegArgs = append(ffmpegArgs, "-hls_segment_filename")
		hls_segment_filename_value := media_output_path + "stream_%v/seg_%02d"
		file_extention := ""
		if j.Output.Segment_format == FMP4 {
			file_extention = ".m4s"
		} else if j.Output.Segment_format == MPEGTS {
			file_extention = ".ts"
		}

		hls_segment_filename_value += file_extention
		ffmpegArgs = append(ffmpegArgs, hls_segment_filename_value)

		ffmpegArgs = append(ffmpegArgs, "-master_pl_name")
		ffmpegArgs = append(ffmpegArgs, media_output_path + HLS_MASTER_PLAYLIST_FILE_NAME)

		ffmpegArgs = append(ffmpegArgs, "-var_stream_map")
		vsr := ""
		for i := range j.Output.Video_outputs {
			vsr = vsr + "v:" + strconv.Itoa(i) + " "
		}

		for i := range j.Output.Audio_outputs {
			vsr = vsr + "a:" + strconv.Itoa(i) + " "
		}

		vsr = vsr[: len(vsr)-1]
		ffmpegArgs = append(ffmpegArgs, vsr)

		variant_playlist_format_value := media_output_path + "stream_%v/stream_%v.m3u8"
		ffmpegArgs = append(ffmpegArgs, variant_playlist_format_value)
	}

    return ffmpegArgs, local_media_output_path_subdirs
}