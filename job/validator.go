package job

import (
	"strconv"
	"errors"
	"ezliveStreaming/utils"
)

var valid_stream_type_values = []string{"hls", "dash"}
var valid_segment_format_values = []string{"fmp4"}
var valid_video_codec_values = []string{"h264", "h265", "av1"}
var valid_detection_codec_values = []string{"h264", "h265"}
var valid_audio_codec_values = []string{"aac", "mp3"}
var valid_bitrate_units = []string{"k", "K"}
var valid_h26x_presets = []string{"placebo", "veryslow", "slower", "slow", "medium", "fast", "faster", "veryfast", "superfast", "ultrafast"}

const max_fragment_duration = 10            // second
const max_segment_duration = 10             // second
const default_time_shift_buffer_depth = 120 // second
const max_time_shift_buffer_depth = 14400   // second
const max_video_outputs = 10
const max_audio_outputs = 5
const max_video_framerate = 60                    // fps
const min_video_framerate = 0                     // fps
const default_detection_frame_rate = 25			  // fps
const max_video_resolution_height = 1080          // pixel
const max_video_resolution_width = 1920           // pixel
const max_video_output_bitrate = 5000.0           // kbps
const max_audio_output_bitrate = 256.0              // kbps
const max_peak_to_average_bitrate_ratio = 2       // X 2
const max_buffersize_to_average_bitrate_ratio = 2 // X 2
const min_libsvtav1_preset = 12
const min_h26x_preset = 5 // "fast"
const min_h26x_crf = 0 // ffmpeg allows crf = 0 (lossless compression). However, this is practically impossible for live encoding.
const practical_min_h26x_crf = 23 // Return a warning when crf is lower than this value.
const max_h26x_crf = 51
const min_av1_crf = 0
const practical_min_av1_crf = 23 // Return a warning when crf is lower than this value.
const max_av1_crf = 63
const min_h26x_threads = 2
const min_av1_threads = 4

func contains_string(a []string, v string) bool {
	r := false
	for _, e := range a {
		if v == e {
			r = true
		}
	}

	return r
}

func contains_int(a []int, v int) bool {
	r := false
	for _, e := range a {
		if v == e {
			r = true
		}
	}

	return r
}

func Validate(j *LiveJobSpec) (error, []string) {
	// A general note:
	// Any data type mismatches were already detected when (json)decoding the http job request body.

	var warnings []string

	// Fatal error. Stream_type is required
	if (*j).Output.Stream_type == "" || !contains_string(valid_stream_type_values, (*j).Output.Stream_type) {
		return errors.New("bad_stream_type"), warnings
	}

	// Default to "fmp4"
	if (*j).Output.Segment_format == "" || !contains_string(valid_segment_format_values, (*j).Output.Segment_format) {
		(*j).Output.Segment_format = "fmp4"
	}

	// Default to 4 seconds
	if (*j).Output.Segment_duration <= 0 || (*j).Output.Segment_duration > max_segment_duration {
		(*j).Output.Segment_duration = 4
	}

	// Default to 2 seconds
	if (*j).Output.Fragment_duration <= 0 ||
		(*j).Output.Fragment_duration > max_fragment_duration {
		(*j).Output.Fragment_duration = 1
	}

	if (*j).Output.Fragment_duration > (*j).Output.Segment_duration {
		(*j).Output.Fragment_duration = (*j).Output.Segment_duration
	}

	// Low_latency_mode defaults to 0. No need to validate.

	// Default to 120 seconds
	if (*j).Output.Time_shift_buffer_depth <= 0 {
		(*j).Output.Time_shift_buffer_depth = default_time_shift_buffer_depth
	}

	// Time_shift_buffer_depth NOT to be higher than 4 hours
	if (*j).Output.Time_shift_buffer_depth > max_time_shift_buffer_depth {
		(*j).Output.Time_shift_buffer_depth = max_time_shift_buffer_depth
	}

	// DRM config is optional
	// Currently, only clear-key DRM is supported so we cannot disable clear key.
	if !((*j).Output.Drm.Protection_system == "" && (*j).Output.Drm.Protection_scheme == "") { // DRM is configured.
		if (*j).Output.Drm.Disable_clear_key != 0 {
			return errors.New("Disable_clear_key_must_be_false"), warnings
		}

		if !((*j).Output.Drm.Protection_system != "" && (*j).Output.Drm.Protection_scheme != "") {
			return errors.New("protection_system_and_scheme_must_both_be_set"), warnings
		}

		if !((*j).Output.Drm.Protection_system == "FairPlay" ||
			(*j).Output.Drm.Protection_system == "Fairplay" ||
			(*j).Output.Drm.Protection_system == "fairplay") {
			return errors.New("protection_system_must_be_FairPlay"), warnings
		}

		if !((*j).Output.Drm.Protection_scheme == "cbcs" ||
			(*j).Output.Drm.Protection_scheme == "CBCS" ||
			(*j).Output.Drm.Protection_scheme == "Cbcs") {
			return errors.New("protection_scheme_must_be_cbcs"), warnings
		}
	}

	// Fatal error. Output bucket is required
	// TODO: Need to verify output bucket (e.g., upload a small file to verify)
	if (*j).Output.S3_output.Bucket == "" {
		return errors.New("missing_output_bucket"), warnings
	}

	// Fatal error. Video_outputs cannot be empty
	// TODO: if we are supporting audio-only transcoding in the future, empty video outputs would become valid.
	if len((*j).Output.Video_outputs) == 0 {
		return errors.New("no_video_output"), warnings
	}

	if len((*j).Output.Video_outputs) > max_video_outputs {
		return errors.New("too_many_video_outputs"), warnings
	}

	// Find the bitrate of the lowest rendition. This will be used when validating object detection params
	var min_rendition_bitrate float64 = max_video_output_bitrate * 1000
	for i := range (*j).Output.Video_outputs {
		vo := (*j).Output.Video_outputs[i]
		err, b := utils.BitrateString2Float64(vo.Bitrate)
		if err != nil {
			return errors.New("bad_video_output_bitrate"), warnings
		}

		if b < min_rendition_bitrate {
			min_rendition_bitrate = b
		}
	}

	for i := range (*j).Output.Video_outputs {
		vo := (*j).Output.Video_outputs[i]
		// Fatal error. Video codec is required
		if vo.Codec == "" ||
			!contains_string(valid_video_codec_values, vo.Codec) {
			return errors.New("bad_video_codec"), warnings
		}

		// Video frame rate validation
		if vo.Framerate < min_video_framerate ||
			vo.Framerate > max_video_framerate {
			return errors.New("video_framerate_out_of_range"), warnings
		}

		err, b := utils.BitrateString2Float64(vo.Bitrate)
		if err != nil {
			return errors.New("bad_video_output_bitrate"), warnings
		}

		// Validate object detection params. 
		// Object detection is ONLY performed on the lowest bitrate rendition.
		if NeedObjectDetection(*j) && b == min_rendition_bitrate {
			detection_frame_rate := (*j).Output.Detection.Ingest_frame_rate
			if vo.Framerate > 0 && detection_frame_rate > vo.Framerate {
				(*j).Output.Detection.Ingest_frame_rate = vo.Framerate
				w := "- Object detection ingest frame rate " + strconv.FormatFloat(detection_frame_rate, 'f', 4, 64) + " cannot be greater than output video frame rate " + strconv.FormatFloat(vo.Framerate, 'f', 4, 64)
				warnings = append(warnings, w)
			}

			if (*j).Output.Detection.Ingest_frame_rate == 0 {
				if vo.Framerate > 0 {
					(*j).Output.Detection.Ingest_frame_rate = vo.Framerate
				} else {
					(*j).Output.Detection.Ingest_frame_rate = default_detection_frame_rate
				}
			}

			// Post-detection video re-encoding only uses h264 or h265, not av1
			if (*j).Output.Detection.Encode_codec == "" {
				(*j).Output.Detection.Encode_codec = "h264"
			} else if !contains_string(valid_detection_codec_values, (*j).Output.Detection.Encode_codec) {
				return errors.New("bad_detection_codec_" + strconv.FormatFloat(detection_frame_rate, 'f', 4, 64) + "_" + strconv.FormatFloat(vo.Framerate, 'f', 4, 64)), warnings
			}

			if (*j).Output.Detection.Encode_preset == "" {
				(*j).Output.Detection.Encode_preset = "veryfast"
			} else if !contains_string(valid_h26x_presets, (*j).Output.Detection.Encode_preset) {
				return errors.New("bad_detection_preset"), warnings
			}

			if (*j).Output.Detection.Encode_crf == 0 {
				(*j).Output.Detection.Encode_crf = 27
			} else if (*j).Output.Detection.Encode_crf < min_h26x_crf || (*j).Output.Detection.Encode_crf > max_h26x_crf {
				return errors.New("bad_detection_crf"), warnings
			} else if (*j).Output.Detection.Encode_crf < practical_min_h26x_crf {
				(*j).Output.Detection.Encode_crf = practical_min_h26x_crf
				w := "- It's not recommended to use crf lower than " + strconv.Itoa(practical_min_h26x_crf) + " to perform object detection reencode"
				warnings = append(warnings, w)
			}
		}

		// Video resolution valiation
		if vo.Width <= 0 || vo.Width > max_video_resolution_width {
			return errors.New("bad_video_resolution_width"), warnings
		}

		if vo.Height <= 0 || vo.Height > max_video_resolution_height {
			return errors.New("bad_video_resolution_height"), warnings
		}

		// Video (average) bitrate validation
		bs := vo.Bitrate[:len(vo.Bitrate)-1]
		bi, err_bitrate := strconv.ParseFloat(bs, 64)
		if err_bitrate != nil {
			return errors.New("bad_video_bitrate"), warnings
		}

		if !contains_string(valid_bitrate_units, vo.Bitrate[len(vo.Bitrate)-1:]) {
			return errors.New("bad_video_bitrate_unit"), warnings
		}

		if bi < 0 || bi > max_video_output_bitrate {
			return errors.New("video_bitrate_out_of_range"), warnings
		}

		// Video max bitrate validation
		mbs := vo.Max_bitrate[:len(vo.Max_bitrate)-1]
		mbi, err_maxbitrate := strconv.ParseFloat(mbs, 64)
		if err_maxbitrate != nil {
			return errors.New("bad_video_max_bitrate"), warnings
		}

		if !contains_string(valid_bitrate_units, vo.Max_bitrate[len(vo.Max_bitrate)-1:]) {
			return errors.New("bad_video_bitrate_unit"), warnings
		}

		if mbi < 0 {
			return errors.New("negative_video_max_bitrate"), warnings
		}

		if mbi > bi*max_peak_to_average_bitrate_ratio {
			w := "- Video max_bitrate should not exceed twice of video average bitrate. "
			warnings = append(warnings, w)
		}

		// Video buffer size validation
		buf_s := vo.Buf_size[:len(vo.Buf_size)-1]
		buf_i, err_bufsize := strconv.ParseFloat(buf_s, 64)
		if err_bufsize != nil {
			return errors.New("bad_video_buffer_size"), warnings
		}

		if !contains_string(valid_bitrate_units, vo.Buf_size[len(vo.Buf_size)-1:]) {
			return errors.New("bad_video_buffer_size_unit"), warnings
		}

		if buf_i < 0 {
			return errors.New("negative_video_buffer_size"), warnings
		}

		if buf_i > bi*max_buffersize_to_average_bitrate_ratio {
			w := "- Video buf_size cannot exceed twice of video average bitrate. "
			warnings = append(warnings, w)
		}

		// Video encoder preset, CRF and threads validation
		if vo.Codec == AV1_CODEC {
			preset, err_preset := strconv.ParseInt(vo.Preset, 10, 64)
			if err_preset != nil {
				return errors.New("bad_libsvtav1_preset"), warnings
			}

			if preset < min_libsvtav1_preset {
				w := "- libsvtav1 presets lower than 12 will not be fast enough for real-time (live) encoding. "
				warnings = append(warnings, w)
			}

			if vo.Crf < practical_min_av1_crf {
				w := "- It's not recommended to use crf lower than " + strconv.Itoa(practical_min_av1_crf)
				warnings = append(warnings, w)
			}

			if vo.Crf < min_av1_crf || vo.Crf > max_av1_crf {
				return errors.New("libsvtav1_crf_out_of_range"), warnings
			}

			if vo.Threads < min_av1_threads {
				w := "- at least " + strconv.Itoa(min_av1_threads) + " threads are needed for av1. "
				warnings = append(warnings, w)
			}
		} else if vo.Codec == H264_CODEC || vo.Codec == H265_CODEC {
			preset_index := -1
			for i, e := range valid_h26x_presets {
				if vo.Preset == e {
					preset_index = i
					break
				}
			}

			if preset_index == -1 {
				return errors.New("invalid_h264_encoder_preset"), warnings
			} else if preset_index < min_h26x_preset {
				w := "- libx264/libx265 presets lower than " + valid_h26x_presets[min_h26x_preset] + " will not be fast enough for real-time (live) encoding. "
				warnings = append(warnings, w)
			}

			if vo.Crf < practical_min_h26x_crf {
				w := "- It's not recommended to use crf lower than " + strconv.Itoa(practical_min_h26x_crf)
				warnings = append(warnings, w)
			}

			if vo.Crf < min_h26x_crf || vo.Crf > max_h26x_crf {
				return errors.New("libsvtav1_crf_out_of_range"), warnings
			}

			if vo.Threads < min_h26x_threads {
				w := "- at least " + strconv.Itoa(min_h26x_threads) + " threads are needed for h26x. "
				warnings = append(warnings, w)
			}
		}
	}

	// Validate audio outputs
	if len((*j).Output.Audio_outputs) == 0 {
		w := "- No audio outputs configured. "
		warnings = append(warnings, w)
		return nil, warnings
	}

	if len((*j).Output.Audio_outputs) > max_audio_outputs {
		return errors.New("too_many_audio_outputs"), warnings
	}

	for i := range (*j).Output.Audio_outputs {
		ao := (*j).Output.Audio_outputs[i]
		// Fatal error. Audio codec is required
		if ao.Codec == "" ||
			!contains_string(valid_audio_codec_values, ao.Codec) {
			return errors.New("bad_audio_codec"), warnings
		}

		// Audio bitrate validation
		bs := ao.Bitrate[:len(ao.Bitrate)-1]
		bi, err_bitrate := strconv.ParseFloat(bs, 64)
		if err_bitrate != nil {
			return errors.New("bad_audio_bitrate"), warnings
		}

		if !contains_string(valid_bitrate_units, ao.Bitrate[len(ao.Bitrate)-1:]) {
			return errors.New("bad_audio_bitrate_unit"), warnings
		}

		if bi < 0 {
			return errors.New("audio_bitrate_out_of_range"), warnings
		}

		if bi > max_audio_output_bitrate {
			w := "- Audio bitrate should not exceed " + strconv.Itoa(max_audio_output_bitrate) + "kbps. "
			warnings = append(warnings, w)
		}
	}

	return nil, warnings
}