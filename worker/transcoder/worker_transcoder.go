// Live transcoding/streaming transcoder
package main

import (
	"container/list"
	"encoding/hex"
	"encoding/json"
	"errors"
	"ezliveStreaming/job"
	"ezliveStreaming/models"
	"ezliveStreaming/s3"
	"ezliveStreaming/utils"
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"bufio"
)

type Upload_item struct {
	File_path                string
	Time_created             time.Time
	Num_retried              int
	Remote_media_output_path string
}

type detectionJob struct {
	Merged_segment_path string
	LiveJob job.LiveJobSpec
	Original_media_data_segment_path string
	Remote_media_output_path string
}

var Log *log.Logger
var upload_list *list.List
var init_segments_to_upload = make(map[string]Upload_item) // map key (string): rendition name; map value: Upload_item representing the init segments
var local_media_output_path string

const transcoder_status_check_interval = "2s"
const stream_file_upload_interval = "0.1s"
const process_detection_job_interval = "0.2s"
const upload_input_info_file_wait_time = "20s"
const max_upload_retries = 3
const max_concurrent_uploads = 5
const max_concurrent_detection_jobs = 1
var num_concurrent_detection_jobs = 0
var original_detection_target_init_segment_path_local string // The original detection target init segment output by Shaka packager (before detection)
var original_detection_target_hls_playlist_path_local string // The original detection target HLS playlist output by Shaka packager (before detection)
var upload_candidate_detection_init_segment string // The new init segment output by Yolo detector which is ready to upload
const init_segment_local_filename = "init.mp4"
const detection_output_init_segment_local_filename = job.Mp4box_segment_template_prefix + "init.mp4" // This must match MP4Box init segment filename
const undefined_bitrate = "undefined"

// The wait time from when a stream file is created by the packager, till when we are safe to upload the file (assuming the file is fully written)
const stream_file_write_delay_ms = 200

var queued_detection_jobs []detectionJob

func manageFfmpegAlone(ffmpegCmd *exec.Cmd) {
	// According to https://pkg.go.dev/os#FindProcess,
	// On Unix systems, function FindProcess always succeeds and returns a Process for the given pid,
	// regardless of whether the process exists.
	// To test whether the process actually exists, see whether p.Signal(syscall.Signal(0)) reports an error.
	process, _ := os.FindProcess(int(ffmpegCmd.Process.Pid))
	errSignal0 := process.Signal(syscall.Signal(0))
	Log.Printf("process.Signal 0 on pid %d returned: %v\n", ffmpegCmd.Process.Pid, errSignal0)

	if errSignal0 != nil {
		errSigterm := process.Signal(syscall.Signal(syscall.SIGTERM))
		Log.Printf("process.Signal.SIGTERM on pid %d returned: %v\n", ffmpegCmd.Process.Pid, errSigterm)
		os.Exit(0)
	}
}

// Monitor ffmpeg and shaka packager
// ffmpeg and packager must both be running.
// If one dies, the other should be killed.
// If neither ffmpeg nor the packager is running, worker_transcoder should exit.
func manageCommands(command1 *exec.Cmd, command2 *exec.Cmd) {
	process1, err1 := os.FindProcess(int(command1.Process.Pid))
	process2, err2 := os.FindProcess(int(command2.Process.Pid))

	if err1 != nil && err2 != nil {
		Log.Println("Neither ffmpeg nor packager is found. Worker_transcoder exiting...")
		os.Exit(0)
	} else if err1 == nil && err2 != nil {
		err := process1.Signal(syscall.Signal(syscall.SIGTERM))
		Log.Printf("process.Signal.SIGTERM on pid %d returned: %v\n", command1.Process.Pid, err)
		// Return instead of os.Exit(0). if SIGTERM fails to kill the process, worker_transcoder will
		// exit the next time this function is called.
		return
	} else if err1 != nil && err2 == nil {
		err := process2.Signal(syscall.Signal(syscall.SIGTERM))
		Log.Printf("process.Signal.SIGTERM on pid %d returned: %v\n", command2.Process.Pid, err)
		return
	}

	err1 = process1.Signal(syscall.Signal(0))
	Log.Printf("process.Signal on pid %d returned: %v\n", command1.Process.Pid, err1)

	err2 = process2.Signal(syscall.Signal(0))
	Log.Printf("process.Signal on pid %d returned: %v\n", command2.Process.Pid, err2)

	if err1 != nil && err2 != nil {
		Log.Println("Neither ffmpeg nor packager is running. Worker_transcoder exiting...")
		os.Exit(0)
	} else if err1 == nil && err2 != nil {
		err := process1.Signal(syscall.Signal(syscall.SIGTERM))
		Log.Printf("process.Signal.SIGTERM on pid %d returned: %v\n", command1.Process.Pid, err)
		return
	} else if err1 != nil && err2 == nil {
		err := process2.Signal(syscall.Signal(syscall.SIGTERM))
		Log.Printf("process.Signal.SIGTERM on pid %d returned: %v\n", command2.Process.Pid, err)
		return
	}
}

func writeKeyFile(key string, keyFileName string) error {
	bin, err := hex.DecodeString(key)
	if err != nil {
		Log.Println("Failed to write key file. Error: ", err)
		return err
	}

	b := []byte(bin)
	err = os.WriteFile(keyFileName, b, 0644)
	if err != nil {
		Log.Println("Failed to write key file. Error: ", err)
	}

	return err
}

func writeKeyInfoFile(k models.KeyInfo, keyInfoFileName string) error {
	b, _ := json.Marshal(k)
	err := os.WriteFile(keyInfoFileName, b, 0644)
	if err != nil {
		Log.Println("Failed to write key info file. Error: ", err)
	}

	return err
}

func createUploadDrmKeyFile(keyInfoStr string, local_media_output_path string, remote_media_output_path string) error {
	var k models.KeyInfo
	bytesKeyInfoSpec := []byte(keyInfoStr)
	err := json.Unmarshal(bytesKeyInfoSpec, &k)
	if err != nil {
		Log.Println("Failed to unmarshal key info (createUploadDrmKeyFile). Error: ", err)
		return err
	}

	// First, write key file to a local path
	err = writeKeyFile(k.Key, local_media_output_path+models.DrmKeyFileName)
	if err != nil {
		return err
	}

	err = writeKeyInfoFile(k, local_media_output_path+models.DrmKeyInfoFileName)
	if err != nil {
		return err
	}

	f, errStat := os.Stat(local_media_output_path + models.DrmKeyInfoFileName)
	if errStat != nil {
		Log.Printf("File %s not found\n", local_media_output_path+models.DrmKeyInfoFileName)
		return errStat
	}

	// Next, upload the local key file to cloud storage
	err = s3.Upload(local_media_output_path+models.DrmKeyFileName, models.DrmKeyFileName, remote_media_output_path)
	if err != nil {
		Log.Printf("Failed to upload %s to %s\n", local_media_output_path+models.DrmKeyFileName, remote_media_output_path)
	} else {
		Log.Printf("Successfully uploaded %d bytes (%s) to S3\n", f.Size(), local_media_output_path+job.Input_json_file_name)
	}

	// Key info file contains key_id and key in plain text.
	// It is only written to local disk for debugging purposes.
	// Do NOT upload to origin!!!
	/*
	   err = s3.Upload(local_media_output_path + models.DrmKeyInfoFileName, models.DrmKeyInfoFileName, remote_media_output_path)
	   if err != nil {
	       Log.Printf("Failed to upload %s to %s\n", local_media_output_path + models.DrmKeyInfoFileName, remote_media_output_path)
	   }
	*/

	return err
}

func set_upload_input_info_timer(local_media_output_path string, remote_media_output_path string) {
	d3, _ := time.ParseDuration(upload_input_info_file_wait_time)
	uploadInputInfoFileTimer := time.NewTimer(d3)
	<-uploadInputInfoFileTimer.C
	uploadInputInfoFile(local_media_output_path, remote_media_output_path)
}

func uploadInputInfoFile(local_media_output_path string, remote_media_output_path string) error {
	f, err := os.Stat(local_media_output_path + job.Input_json_file_name)
	if err != nil {
		Log.Printf("File %s not found\n", local_media_output_path+job.Input_json_file_name)
		set_upload_input_info_timer(local_media_output_path, remote_media_output_path)
		return err
	}

	if f.Size() == 0 {
		Log.Printf("Empty file %s\n", local_media_output_path+job.Input_json_file_name)
		set_upload_input_info_timer(local_media_output_path, remote_media_output_path)
		return errors.New("do_not_upload_empty_file")
	}

	err = s3.Upload(local_media_output_path+job.Input_json_file_name, job.Input_json_file_name, remote_media_output_path)
	if err != nil {
		Log.Printf("Failed to upload %s to %s\n", local_media_output_path+job.Input_json_file_name, remote_media_output_path)
		return err
	} else {
		Log.Printf("Successfully uploaded %d bytes (%s) to S3\n", f.Size(), local_media_output_path+job.Input_json_file_name)
	}

	return nil
}

// Scan upload_list in fifo order and upload qualified stream files to cloud storage
// A stream file is qualified for upload if all of the following conditions are met,
// - it is created more than "stream_file_write_delay_ms (200ms)" ago,
// - its upload retry count does not exceed "max_upload_retries (3)",
// - it is within the first "max_concurrent_uploads" items in upload_list.
func uploadFiles() {
	i := 1
	var f Upload_item
	var prev_e *list.Element
	prev_e = nil
	for e := upload_list.Front(); e != nil; e = e.Next() {
		if prev_e != nil {
			upload_list.Remove(prev_e)
		}

		f = Upload_item(e.Value.(Upload_item))

		fs, err := os.Stat(f.File_path)
		if err != nil {
			Log.Printf("File %s not found\n", f.File_path)
		}

		time_created := f.Time_created.UnixMilli()
		now := time.Now().UnixMilli()
		Log.Printf("%d - Upload item: \n file: %s (size: %d bytes)\n time_created: %d (time_elapsed: %d)\n num_retried: %d\n remote_path: %s\n", now, f.File_path, fs.Size(), time_created, now-time_created, f.Num_retried, f.Remote_media_output_path)

		if now-time_created > stream_file_write_delay_ms {
			if i > max_concurrent_uploads {
				Log.Printf("Current upload: %d > Max. uploads: %d. Let's upload later.\n", i, max_concurrent_uploads)
				break
			} else {
				Log.Printf("Current upload: %d < Max. uploads: %d. Proceed to upload.\n", i, max_concurrent_uploads)
			}

			// Do not call s3 upload SDK in a go routine because it does not seem to be thread-safe.
			//go func() {
			var err error
			err = nil
			if f.Num_retried < max_upload_retries {
				Log.Printf("Num. retried: %d < max_retries: %d. Stream file %s uploading...\n", f.Num_retried, max_upload_retries, f.File_path)
				i++

				err = uploadOneFile(f.File_path, f.Remote_media_output_path)
			} else {
				Log.Printf("Num. retried: %d < max_retries: %d. Drop upload of stream file %s due to exceeding max_retries.\n", f.Num_retried, max_upload_retries, f.File_path)
			}

			if err != nil {
				f.Num_retried++
				upload_list.PushBack(f)
			}
			//}()

			prev_e = e
		} else {
			Log.Printf("Item %s is NOT ready to be uploaded.\n", f.File_path)
			prev_e = nil
		}
	}

	if prev_e != nil {
		upload_list.Remove(prev_e)
	}
}

func uploadOneFile(local_file string, remote_path_base string) error {
	f, err := os.Stat(local_file)
	if err != nil {
		Log.Printf("File %s not found\n", local_file)
		return err
	}

	posLastSingleSlash := strings.LastIndex(local_file, "/")
	file_name := local_file[posLastSingleSlash+1:]
	file_path := local_file[:posLastSingleSlash-1]

	rendition_name := ""
	if isMediaDataSegment(local_file) || isFmp4InitSegment(local_file) || isHlsVariantPlaylist(local_file) {
		// Depending on video transcoding specification by the user, worker_transcoder may choose to use
		// "ffmpeg + shaka" or "ffmpeg-alone" (e.g., when "av1" video codec is specified) to transcode and package.
		// The stream output structure are the same for ffmpeg and shaka-packager.

		// Shaka packager output structure:
		// master playlist: master.m3u8
		// variant playlist: [rendition_name]/playlist.m3u8, e.g., video_500k/playlist.m3u8
		// data segments: [rendition_name]/seg_[number].m4s, e.g., video_500k/seg_10.m4s
		// init segments: [rendition_name]/init.mp4, e.g., video_500k/init.mp4

		// ffmpeg output structure:
		// master playlist: master.m3u8
		// variant playlist: [rendition_name]/playlist.m3u8, e.g., stream_0/playlist.m3u8
		// data segments: [rendition_name]/seg_[number].m4s, e.g., stream_0/seg_10.m4s
		// init segments: [rendition_name]/init.mp4, e.g., stream_0/init.mp4

		// Extract [rendition_name] from the paths, except for master.m3u8 which is stored directly under the path base.
		posSecondLastSingleSlash := strings.LastIndex(file_path, "/")
		rendition_name = local_file[posSecondLastSingleSlash+1:posLastSingleSlash] + "/"
	}

	Log.Printf("Uploading %s to %s\n", local_file, remote_path_base+rendition_name+file_name)
	err = s3.Upload(local_file, file_name, remote_path_base+rendition_name)
	if err != nil {
		Log.Printf("Failed to upload: %s to %s. Error: %v\n", local_file, remote_path_base+rendition_name+file_name, err)
		return err
	} else {
		Log.Printf("Successfully uploaded %d bytes (%s) to S3\n", f.Size(), local_file)
	}

	// This is the first media data segment. Let's also upload the init segment of this rendition.
	// FFmpeg (AV1) media data segment template: "stream_%v/seg_%05d". The first segment is seg_00000.m4s.
	// Shaka packager (H.26x) template: "video_%v/seg_$Number$.m4s". The first segment is seg_1.m4s.
	// MP4Box (object detection) template: "video_%v/segment_$Number$.m4s". The first segment is segment_1.m4s.
	// TODO: remove dependency on data segment template.
	if isMediaDataSegment(local_file) && (strings.Contains(local_file, "seg_00000.") || strings.Contains(local_file, "seg_1.") || strings.Contains(local_file, "segment_1.")) {
		item, ok := init_segments_to_upload[rendition_name[:len(rendition_name)-1]]
		if !ok {
			Log.Printf("Failed to find init segment item with rendition_name = %s\nAre you sure %s is a valid path and is the first media data segment?\n", rendition_name[:len(rendition_name)-1], local_file)
			return nil // This is NOT a fatal error, let's return nil.
		}

		Log.Printf("Add %s to UploadList\n", item.File_path)
		upload_list.PushBack(item)
	}

	return err
}

func isStreamFile(file_name string) bool {
	return (strings.Contains(file_name, ".m3u8") ||
		strings.Contains(file_name, ".mpd") ||
		strings.Contains(file_name, ".mp4") ||
		strings.Contains(file_name, ".ts") ||
		strings.Contains(file_name, ".m4s")) &&
		!strings.Contains(file_name, ".tmp")
}

// For H.26x output, media data segments always look like "seg_x.m4s" (template: "seg_$number$.m4s")
// For AV1 output, media data segments always look like "seg_xxxxx.m4s" (template: "seg_%5d.m4s")
func isMediaDataSegment(file_name string) bool {
	return (strings.Contains(file_name, ".ts") ||
		strings.Contains(file_name, ".m4s")) &&
		!strings.Contains(file_name, ".tmp")
}

func isFmp4InitSegment(file_name string) bool {
	return strings.Contains(file_name, ".mp4") &&
		!strings.Contains(file_name, ".tmp")
}

func isHlsVariantPlaylist(file_name string) bool {
	return strings.Contains(file_name, "playlist.m3u8") &&
		!strings.Contains(file_name, ".tmp")
}

func isHlsmasterPlaylist(file_name string) bool {
	return strings.Contains(file_name, "master.m3u8") &&
		!strings.Contains(file_name, ".tmp")
}

// The detection target rendition is an encoder output rendition that is used for object detection
func isDetectionTarget(file_name string, detection_target_bitrate string) bool {
	return strings.Contains(file_name, detection_target_bitrate)
}

// A media data segment of the detection target rendition
func isDetectionTargetTypeMediaDataSegment(file_name string, detection_target_bitrate string) bool {
	return strings.Contains(file_name, detection_target_bitrate) && isMediaDataSegment(file_name) && strings.Contains(file_name, "seg_")
}

// A media init segment of the detection target rendition. 
func isDetectionTargetTypeMediaInitSegment(file_name string, detection_target_bitrate string) bool {
	return strings.Contains(file_name, detection_target_bitrate) && strings.Contains(file_name, init_segment_local_filename) && !strings.Contains(file_name, detection_output_init_segment_local_filename)
}

// A variant playlist of the detection target rendition
func isDetectionTargetTypeHlsVariantPlaylist(file_name string, detection_target_bitrate string) bool {
	return strings.Contains(file_name, detection_target_bitrate) && isHlsVariantPlaylist(file_name)
}

func merge_init_and_data_segments(init_segment_path string, data_segment_path string) (string, error) {
	// Wait 200ms before data segment becomes fully written
	time.Sleep(200 * time.Millisecond)
	Log.Printf("Merging init segment %s and data segment %s\n", init_segment_path, data_segment_path)
	
	var merged_segment_path string = ""
	var merged_segment_buffer []byte
	var err error
	var bytes_init []byte
	var bytes_data []byte
	bytes_init, err = utils.Read_file(init_segment_path)
	if err != nil {
		Log.Printf("Failed to read detection output init segment: %s. Error: %v", init_segment_path, err)
		return merged_segment_path, err
	}

	bytes_data, err = utils.Read_file(data_segment_path)
	if err != nil {
		Log.Printf("Failed to read detection output data segment: %s. Error: %v", data_segment_path, err)
		return merged_segment_path, err
	}

	if len(bytes_data) == 0 {
		Log.Printf("Empty detection output data segment: %s", data_segment_path)
		return merged_segment_path, errors.New("empty_detection_media_data")
	}

	merged_segment_buffer = append(merged_segment_buffer, bytes_init...)
	merged_segment_buffer = append(merged_segment_buffer, bytes_data...)

	// Change file extension from ".m4s" to ".merged" so that the merged segment 
	// would not be uploaded
	merged_segment_path = utils.Change_file_extension(data_segment_path, ".merged")
	utils.Write_file(merged_segment_buffer, merged_segment_path)

	return merged_segment_path, nil
}

// This function calls MP4Box to convert a single MP4 segment to a fMP4 stream consisting exactly one init segment and one fMP4 segment.
// MP4Box command: MP4Box -dash 4000 -segment-name 'segment_$Number$' -out 1.m3u8 /tmp/1.mp4
// Output: "1.mpd  segment_1.m4s  segment_2.m4s  segment_3.m4s  segment_4.m4s  segment_5.m4s  segment_6.m4s  segment_7.m4s  segment_8.m4s  segment_init.mp4"
func mp4_to_fmp4(input string, seg_duration int) (string, string, error) {
	mp4box_init_segment_filename := utils.Get_path_dir(input) + "/" + detection_output_init_segment_local_filename

	// The MP4Box command guarantees the output segment name would always be "segment_1.m4s"
	// The input MP4 file consists of only one media data segment. The MP4Box output will consist of only one segment as well, hence the segment number will always be "1".
	mp4box_media_data_segment_filename := utils.Get_path_dir(input) + "/segment_1.m4s"
	var err error

	converterArgs := job.GenerateFmp4ConversionCommand(input, seg_duration)
	Log.Println("Fmp4Converter arguments: ")
	Log.Println(job.ArgumentArrayToString(converterArgs))

	converterCmd := exec.Command("MP4Box", converterArgs...)
	var errConverter error = nil
	var out []byte
	out, errConverter = converterCmd.CombinedOutput() // This line blocks when converterCmd launch succeeds
	if errConverter != nil {
		Log.Println("Errors starting converter: ", errConverter, " converter output: ", string(out))
		return mp4box_init_segment_filename, mp4box_media_data_segment_filename, errors.New("error_starting_converter")
	}

	return mp4box_init_segment_filename, mp4box_media_data_segment_filename, err
}

func getRenditionNameFromPath(path string) string {
	s := ""
	posLastSingleSlash := strings.LastIndex(path, "/")
	if posLastSingleSlash == -1 {
		return s
	}

	t := path[:posLastSingleSlash]

	posSecondLastSingleSlash := strings.LastIndex(t, "/")
	if posSecondLastSingleSlash == -1 {
		return s
	}

	return t[posSecondLastSingleSlash+1:]
}

func addToUploadList(file_path string, remote_media_output_path string) {
	var it Upload_item
	it.File_path = file_path
	it.Time_created = time.Now()
	it.Num_retried = 0
	it.Remote_media_output_path = remote_media_output_path

	// Add fmp4 init segments to init_segments_to_upload.
	// Add an init segment to upload_list when we upload the first media data segment.
	// Why?
	// For AV1 video, FFmpeg takes long time (5-10 seconds) between when it creates "init.mp4" on disk
	// and when it actually writes "init.mp4" along with the first data segment and the variant playlist.
	// Therefore, waiting stream_file_write_delay_ms time units (e.g., around 200ms) is not long enough
	// for "init.mp4" to be ready for s3 upload. If we upload now, it will be an empty file in the bucket.
	// The fix:
	// Let's wait when the first media data segment becomes ready at which time "init.mp4" is guaranteed
	// to be ready for upload. So, we add init.mp4 to init_segments_to_upload.
	if isFmp4InitSegment(file_path) {
		rendition_name := getRenditionNameFromPath(file_path)
		if rendition_name != "" {
			Log.Printf("Add %s to init segment table under rendition name = %s\n", file_path, rendition_name)
			init_segments_to_upload[rendition_name] = it
		} else {
			Log.Printf("Failed to add %s to init segment table. Invalid rendition name: %s\n", file_path, rendition_name)
		}
	} else { // Add media data segments to upload_list
		Log.Printf("Add %s to UploadList\n", file_path)
		upload_list.PushBack(it)
	}
}

// Run detection on ".merged" segment file (containing both fmp4 initialization 
// section and media data section), and output a ".detected" segment file
func run_detection(j job.LiveJobSpec, input_segment_path string) (string, error) {
	// Use file extension ".detected" so that it would not be uploaded
	// ".detected" files contain both an fmp4 initialization section and media
	// data section, and are NOT immediately uploadable. We need to 
	// 1. Extract the initialization section from the ".detected" file 
	// and output a new initialization segment. This needs to be done only once.
	// 2. Strip off the initialization section from every ".detected" file.
	detected_segment_path := utils.Change_file_extension(input_segment_path, ".detected")
	Log.Printf("Running detection on input segment: %s. Output path: %s\n", input_segment_path, detected_segment_path)

	detectorArgs := job.GenerateDetectionCommand(j.Output.Detection.Input_video_frame_rate, input_segment_path, detected_segment_path)
	Log.Println("Detector arguments: ")
	Log.Println(job.ArgumentArrayToString(detectorArgs))

	detectorCmd := exec.Command("sh", detectorArgs...)
	var errDetector error = nil
	var out []byte
	out, errDetector = detectorCmd.CombinedOutput() // This line blocks when detectorCmd launch succeeds
	if errDetector != nil {
		errMessage := string(out)
		Log.Println("Errors starting detector: ", errDetector, " detector output: ", errMessage)
		
		ignoreError := false
		// The following error can be ignored as it does not stop detector from running successfully.
		// "[W1213 11:31:21.107731264 NNPACK.cpp:61] Could not initialize NNPACK! Reason: Unsupported hardware."
		if strings.Contains(errMessage, "NNPACK") {
			ignoreError = true
		}

		if !ignoreError {
			return detected_segment_path, errors.New("error_starting_detector")
		}
	}

	// When detection finishes, the output 
	return detected_segment_path, nil
}

// Pop a detection job from the queue and execute it
func executeNextDetectionJob() {
	if len(queued_detection_jobs) == 0 {
		return
	}

	var frontJob detectionJob
	frontJob = queued_detection_jobs[0]

	if num_concurrent_detection_jobs >= max_concurrent_detection_jobs {
		Log.Printf("Job not executed!! Cannot process more than %d jobs (but %d jobs running currently) at same time. Wait until current jobs finish.\n", max_concurrent_detection_jobs, num_concurrent_detection_jobs)
		return
	}

	num_concurrent_detection_jobs = num_concurrent_detection_jobs + 1
	// Pop out the front job from queue
	queued_detection_jobs = queued_detection_jobs[1:]

	// Run detection in a separate thread
	go func() {
		Log.Printf("Exexuting new detection job. Input path: %s.\n", frontJob.Merged_segment_path)
		executeDetectionJob(frontJob) 
	}()
}

func executeDetectionJob(dj detectionJob) {
	// Run Yolo object detection on the merged segment (dj.Merged_segment_path) using the given detection configuration (dj.LiveJob)
	// For each input file (dj.Merged_segment_path), the detector outputs a detected segment file (detection_output_segment_path)
	detection_start_time_ms := time.Now().UnixMilli()
	detection_output_segment_path, err := run_detection(dj.LiveJob, dj.Merged_segment_path)
	detection_end_time_ms := time.Now().UnixMilli()
	if err != nil {
		Log.Printf("Failed to run detection on input = %s. Error: %v\n", dj.Merged_segment_path, err)
		// TODO: When detection fails, we shall just show the original non-detected segment.
		//       1. Rename seg_xxxxx.m4s to segment_xxxxx.m4s and upload
		//       2. Update playlist accordingly and upload
		return
	}

	num_concurrent_detection_jobs = num_concurrent_detection_jobs - 1
	Log.Printf("Object detection completed in %d milliseconds. Detection output media segment: %s\n", detection_end_time_ms - detection_start_time_ms, detection_output_segment_path)

	// Delete the merged segment to save space as it is no longer needed
	Log.Printf("Deleting merged segment: %s\n", dj.Merged_segment_path)
	os.Remove(dj.Merged_segment_path)

	// Convert detected mp4 segment to fragmented mp4 format by spliting into an init seg and a media data seg 
	Log.Printf("Converting detected mp4 segment: %s to fmp4 segment\n", detection_output_segment_path)
	new_init_segment_path, new_media_segment_path, err_conversion := mp4_to_fmp4(detection_output_segment_path, dj.LiveJob.Output.Segment_duration)

	// Delete detected mp4 segment which is an intermediate file
	Log.Printf("Deleting detected mp4 segment: %s\n", detection_output_segment_path)
	os.Remove(detection_output_segment_path)

	if err_conversion != nil {
		Log.Printf("Failed to convert detected segment: %s. Error: %v\n", detection_output_segment_path, err_conversion)
		// TODO: When detection fails, we shall just show the original non-detected segment.
		//       1. Rename seg_xxxxx.m4s to segment_xxxxx.m4s and upload
		//       2. Update playlist accordingly and upload
		return
	}

	// Upload the new init segment only once
	if upload_candidate_detection_init_segment == "" {
		upload_candidate_detection_init_segment = new_init_segment_path
		addToUploadList(upload_candidate_detection_init_segment, dj.Remote_media_output_path)
	} 

	// Rename detection output media segment to follow "segment_%d.m4s" template. 
	// For example, "/tmp/output_b11b6b55-9306-4212-a15a-3d4aa79d3fb1/video_150k/seg_6.m4s" is renamed to "/tmp/output_b11b6b55-9306-4212-a15a-3d4aa79d3fb1/video_150k/segment_6.m4s"
	// "segment_%d.m4s" will be found by function watchStreamFiles. However, function isDetectionTargetTypeMediaDataSegment will filter them out.
	pos_seg := strings.LastIndex(dj.Original_media_data_segment_path, "seg_") 
	pos_underscore := strings.LastIndex(dj.Original_media_data_segment_path, "_") 
	// Rename segment while still keeps the segment number.
	upload_candidate_detection_media_data_segment := dj.Original_media_data_segment_path[:pos_seg] + "segment_" + dj.Original_media_data_segment_path[pos_underscore+1 :]
	os.Rename(new_media_segment_path, upload_candidate_detection_media_data_segment)

	// Upload local media data segment file
	Log.Printf("Uploading detection output media data segment: %s\n", upload_candidate_detection_media_data_segment)
	addToUploadList(upload_candidate_detection_media_data_segment, dj.Remote_media_output_path)

	// Add the detected segment to the end (live playhead) of the detection target playlist
	// TODO: Ideally, a segment should be added to the playlist ONLY after it is successfully uploaded.
	err, upload_candidate_detection_playlist := updatePlaylistNewDetectedSegment(original_detection_target_hls_playlist_path_local, dj.Original_media_data_segment_path, upload_candidate_detection_media_data_segment)
	if err != nil {
		Log.Printf("Failed to update playlist to add newly detected segment %d\n", upload_candidate_detection_playlist)
		return
	}

	Log.Printf("Uploading detection HLS playlist: %s\n", upload_candidate_detection_playlist)
	addToUploadList(upload_candidate_detection_playlist, dj.Remote_media_output_path)
}

func updatePlaylistNewDetectedSegment(playlistFile string, original_segment_path string, detected_segment_path string) (error, string) {
	pos_dot := strings.LastIndex(playlistFile, ".")
	updated_playlist_path := playlistFile[: pos_dot] + "_detected" + playlistFile[pos_dot :]

	// Load input playlist
	playlist, err := os.Open(playlistFile)
	if err != nil {
		Log.Printf("Failed to read HLS variant playerlist: %s. Error: %v", playlistFile, err)
		return errors.New("error_reading_playlist"), updated_playlist_path
	}

	defer playlist.Close()
	scanner := bufio.NewScanner(playlist)

	// Load output playlist
	newPlaylist, err := os.OpenFile(updated_playlist_path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		Log.Printf("Failed to create new HLS variant playerlist: %s. Error: %v", updated_playlist_path, err)
        return err, updated_playlist_path
    }

	defer newPlaylist.Close()
	writer := bufio.NewWriter(newPlaylist)

	segment_found_in_playlist := false
	// Scan every line until we reach the line containing URI of the latest detected media data segment
	// Write every line before the target line (include the target line) to the output playlist.
	// Then, skip all following lines which essentially skip all the following media segments.
	// Any segments before the latest one are not ready to stream yet.
	for scanner.Scan() {
		line := scanner.Text()

		pos_lastslash := strings.LastIndex(original_segment_path, "/")
		original_segment_name := original_segment_path[pos_lastslash+1 :]
		if strings.Contains(line, original_segment_name) {
			segment_found_in_playlist = true
			writer.WriteString(line + "\n")
			break
		}

		writer.WriteString(line + "\n")
	}

	if !segment_found_in_playlist {
		Log.Printf("Failed to update detection target variant playlist %s. Segment %s not found\n", playlistFile, original_segment_path)
		return errors.New("segment_not_found_in_playlist"), updated_playlist_path
	}

	writer.Flush()
	return nil, updated_playlist_path
	
	/*if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}*/
}

// https://github.com/fsnotify/fsnotify
func watchStreamFiles(j job.LiveJobSpec, watch_dirs []string, remote_media_output_path string, ffmpegAlone bool, detection_target_bitrate string) error {
	// Create the upload list: the running list of stream files to upload to cloud.
	upload_list = list.New()

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	defer watcher.Close()

	// Start listening for events.
	go func() {
		for {
			select {
				case event, ok := <-watcher.Events:
					if !ok {
						Log.Println("Failed to receive a file system event (watchStreamFiles)")
						continue
					}

					// After a stream file (e.g., media segments, playlists) is created on disk, add the file to
					// the upload list to make it ready for upload. The file will be uploaded in the next file
					// upload opportunity
					if event.Op == fsnotify.Create {
						if isStreamFile(event.Name) { 
							// The file is a stream file but not a detection target, upload it right away
							if !isDetectionTarget(event.Name, detection_target_bitrate) {
								addToUploadList(event.Name, remote_media_output_path)
								continue
							}
						} else { // The file is not a stream file, do nothing
							Log.Printf("Skip %s from uploading - Not a stream file\n", event.Name)
							continue
						}

						// The file is a stream file and is a detection target media data segment, do the following,
						// - Merge it with the init segment,
						// - Save the merged file to disk,
						// - Run Yolo inference (detection) on the merged file,
						// - Upload it.

						// The following diagram shows the object detection workflow
						// filename in parenthesis are intermediate files that will be replaced or removed
						// seg.mp4 and init_detection.mp4 are annotated by Yolo and uploaded
						// "init.mp4" is also an intermediate file, but it is always needed for detection.
						
						//          merge             detect               split
						// init.mp4 -----> (seg.merged) --> (seg.detected) -----> seg.m4s 
						//            |                                    |
						// (seg.m4s)---                                    --> init.detected.mp4
						if isDetectionTargetTypeMediaDataSegment(event.Name, detection_target_bitrate) {
							Log.Printf("Media data segment to detect: %s\n", event.Name)
							if original_detection_target_init_segment_path_local == "" {
								Log.Printf("Not ready to merge data segment %s due to missing init segment\n", event.Name)
								// TODO: When merge fails, shall we show a slate segment instead?
								continue
							}

							// Merge init and media data segments to a single inited media data segment.
							merged_segment_path, err := merge_init_and_data_segments(original_detection_target_init_segment_path_local, event.Name)
							if err != nil {
								Log.Printf("Failed to merge detection target init and data segments. Error: %v\n", err)
								return
							}
							
							// Delete the original media data segment as it is already merged.
							// Do not delete the original init segment.
							Log.Printf("Deleting original media segment: %s\n", event.Name)
							os.Remove(event.Name)

							// Create a new detection job then enqueue
							var dj detectionJob
							dj.Merged_segment_path = merged_segment_path // Merged segment: this is the input file to the detector
							dj.LiveJob = j // Live job spec: this includes detection configuration
							dj.Original_media_data_segment_path = event.Name // The path to the original media data segment. Although this file was already deleted, the path name contains segment sequence number which is necessary for naming the detected data segment.
							dj.Remote_media_output_path = remote_media_output_path // The S3 media output path: the detected segment will be uploaded right after detection

							queued_detection_jobs = append(queued_detection_jobs, dj)
						} else if isDetectionTargetTypeMediaInitSegment(event.Name, detection_target_bitrate) { // The file is the init segment of the encoder detection output, save the file path as we will need to merge the init seg with media data segs.
							Log.Printf("Media init segment to detect: %s", event.Name)
							original_detection_target_init_segment_path_local = event.Name
							// Do not upload original detection init segment, e.g., init.mp4. 
							// We only upload the new detected init segment, e.g., init.detected.mp4.
						} else if isDetectionTargetTypeHlsVariantPlaylist(event.Name, detection_target_bitrate) { // The file is the variant playlist of the detection output, update it.
							Log.Printf("HLS variant playerlist to detect: %s", event.Name)
							original_detection_target_hls_playlist_path_local = event.Name
						}
					}
				case _, ok := <-watcher.Errors:
					if !ok {
						Log.Println("Error while receiving an file system event (watchStreamFiles)")
						continue
					}
			}
		}
	}()

	// Add all the watch directories (local media output path and all the subdirs)
	Log.Printf("Watching the following (sub)directories:")
	for _, d := range watch_dirs {
		Log.Printf("  - %s\n", d)
		err = watcher.Add(d)
		if err != nil {
			Log.Printf("Failed to watch file path: %s. Error: %v\n", d, err)
		}
	}

	<-make(chan struct{})
	return err
}

var ffmpegAlone bool

// worker_transcoder -file=job.json -job_id=abcdef
// worker_transcoder -param=[job_json] -job_id=abcdef
func main() {
	jobIdPtr := flag.String("job_id", "", "input job id")
	jobSpecPathPtr := flag.String("file", "", "input job spec file")
	jobSpecStringPtr := flag.String("param", "", "input job spec string")
	drmPtr := flag.String("drm", "", "DRM key info")
	flag.Parse()

	var j job.LiveJobSpec
	if *jobSpecStringPtr != "" {
		fmt.Println("Reading job spec from command line argument: ", *jobSpecStringPtr)
		bytesJobSpec := []byte(*jobSpecStringPtr)
		json.Unmarshal(bytesJobSpec, &j)
	} else if *jobSpecPathPtr != "" {
		fmt.Println("Reading job spec from: ", *jobSpecPathPtr)
		jobSpecFile, err := os.Open(*jobSpecPathPtr)
		if err != nil {
			fmt.Println("Failed to open worker_transcoder spec file. Error: ", err)
			return
		}

		defer jobSpecFile.Close()
		bytesJobSpec, _ := ioutil.ReadAll(jobSpecFile)
		json.Unmarshal(bytesJobSpec, &j)
	} else {
		log.Fatal("Error: please provide job spec string or path to job spec file")
		return
	}

	logName := "/home/streamer/log/worker_transcoder_" + *jobIdPtr + ".log"
	var logfile, err1 = os.Create(logName)
	if err1 != nil {
		fmt.Println("Exiting... Failed to create log file (worker_transcoder)")
		return
	}

	Log = log.New(logfile, "", log.LstdFlags)

	// Shaka packager writes stream files to local storage given by "local_media_output_path".
	// A file watcher (fsnotify) is responsible for checking new stream files written to the file system
	// and uploading them to cloud storage
	local_media_output_path = ("/tmp/" + job.Media_output_path_prefix + *jobIdPtr + "/")
	err1 = os.Mkdir(local_media_output_path, 0777)
	if err1 != nil {
		Log.Println("Failed to mkdir: ", local_media_output_path, " Error: ", err1)
		os.Exit(1)
	}

	// When output video codec is "av1", start ffmpeg only to perform both transcoding and packaging.
	// Do NOT start Shaka packager. As of 05/2024, Shaka packager does not recognize AV1 encoded video
	// contained in MPEG-TS stream. When ingesting AV1 in MPEG-TS, Shaka packager returns error:
	// "mp2t_media_parser.cc:342] Ignore unsupported MPEG2TS stream type 0x0x6"
	if job.HasAV1Output(j) {
		ffmpegAlone = true
	} else {
		ffmpegAlone = false
	}

	var local_media_output_path_subdirs []string
	var packagerArgs []string
	var ffmpegArgs []string
	var out []byte
	var errEncoder error
	var packagerCmd *exec.Cmd
	var ffmpegCmd *exec.Cmd
	packagerCmd = nil
	ffmpegCmd = nil
	remote_media_output_path_base := j.Output.S3_output.Bucket + "/output_" + *jobIdPtr + "/"

	// If object detection is configured, add an extra output rendition for object detection
	if job.NeedObjectDetection(j) {
		job.AddDetectionVideoOutput(&j)
	}

	if !ffmpegAlone {
		// Start Shaka packager first
		packagerArgs, local_media_output_path_subdirs = job.JobSpecToShakaPackagerArgs(*jobIdPtr, j, local_media_output_path, *drmPtr)
		Log.Println("Shaka packager arguments: ")
		Log.Println(job.ArgumentArrayToString(packagerArgs))

		// TODO: File path of the packager binary needs to be added to the PATH env-var
		packagerCmd = exec.Command("packager", packagerArgs...)
		errEncoder = nil
		go func() {
			out, errEncoder = packagerCmd.CombinedOutput() // This line blocks when packagerCmd launch succeeds
			if errEncoder != nil {
				Log.Println("Errors starting Shaka packager: ", errEncoder, " packager output: ", string(out))
				// os.Exit(1) // Do not exit worker_transcoder here since ffmpeg also needs to be stopped after the packager is stopped. Let function manageCommand() to handle this.
			}
		}()

		// Wait 100ms before Shaka packager fully starts
		time.Sleep(100 * time.Millisecond)
		if errEncoder != nil {
			Log.Println("Errors starting Shaka packager: ", errEncoder, " packager output: ", string(out))
			//os.Exit(1)
		}

		// If clear-key DRM is configured for the job, create and upload a key file to cloud storage
		if *drmPtr != "" {
			errUploadKey := createUploadDrmKeyFile(*drmPtr, local_media_output_path, remote_media_output_path_base)
			if errUploadKey != nil {
				Log.Println("Failed to create/upload key file. Error: ", errUploadKey)
				// TODO: This is a critical error - Stream files will not be decrypted and played when clear-key DRM is used.
				//       Should worker_transcoder exit?
			}
		}

		// Start ffmpeg ONLY if Shaka packager is running
		ffmpegArgs := job.JobSpecToFFmpegArgs(j, local_media_output_path)
		Log.Println("FFmpeg arguments: ")
		Log.Println(job.ArgumentArrayToString(ffmpegArgs))

		ffmpegCmd = exec.Command("ffmpeg", ffmpegArgs...)

		errEncoder = nil
		go func() {
			out, errEncoder = ffmpegCmd.CombinedOutput() // This line blocks when ffmpegCmd launch succeeds
			if errEncoder != nil {
				Log.Println("Errors starting ffmpeg: ", errEncoder, " ffmpeg output: ", string(out))
				//os.Exit(1)
			}
		}()

		// Wait 100ms before ffmpeg fully starts
		time.Sleep(100 * time.Millisecond)
		if errEncoder != nil {
			Log.Println("Errors starting ffmpeg: ", errEncoder, " ffmpeg output: ", string(out))
			//os.Exit(1)
		}
	} else { // Start ffmpeg-alone (e.g., AV1 transcoding)
		ffmpegArgs, local_media_output_path_subdirs = job.JobSpecToEncoderArgs(j, local_media_output_path)
		Log.Println("FFmpeg-alone arguments: ")
		Log.Println(job.ArgumentArrayToString(ffmpegArgs))

		ffmpegCmd = exec.Command("ffmpeg", ffmpegArgs...)
		ffmpegCmd.Dir = local_media_output_path
		go func() {
			out, errEncoder = ffmpegCmd.CombinedOutput() // This line blocks when ffmpegCmd launch succeeds
			if errEncoder != nil {
				Log.Println("Errors starting ffmpeg-alone: ", errEncoder, " ffmpeg-alone output: ", string(out))
				//os.Exit(1)
			}
		}()

		// Wait 100ms before ffmpeg fully starts
		time.Sleep(100 * time.Millisecond)
		if errEncoder != nil {
			Log.Println("Errors starting ffmpeg-alone: ", errEncoder, " ffmpeg-alone output: ", string(out))
			//os.Exit(1)
		}
	}

	// Start ffprobe to receive passed-through input stream from ffmpeg and extract input stream info
	ffprobeArgs := job.GenerateFfprobeArgs(j, local_media_output_path)
	Log.Println("FFprobe arguments: ")
	Log.Println(job.ArgumentArrayToString(ffprobeArgs))

	ffprobeCmd := exec.Command("sh", ffprobeArgs...)

	var errFfprobe error
	errFfprobe = nil
	go func() {
		out, errFfprobe = ffprobeCmd.CombinedOutput() // This line blocks when ffprobeCmd launch succeeds
		if errFfprobe != nil {
			Log.Println("Errors starting ffprobe: ", errFfprobe, " ffprobe output: ", string(out))
		}
	}()

	// Wait 100ms before ffprobe fully starts
	time.Sleep(100 * time.Millisecond)
	if errFfprobe != nil {
		Log.Println("Errors starting ffprobe: ", errFfprobe, " ffprobe output: ", string(out))
	}

	// 1. FFmpeg pipes the passed-through (original) input stream over mpegts-udp to FFprobe.
	// 2. FFprobe analyzes the input stream and output input_info.json to the job's base media output path.
	// 3. Worker_transcoder uploads input_info.json to S3, both frontend and backend can download and use the info.
	// 4. FFprobe analyzes the input stream, output input_info.json then exit immediately.
	//    There is no need for worker_transcoder to kill it.
	//    Also, FFprobe exits and does not cause FFmpeg to crash.
	// 5. It takes FFprobe ~5 seconds to analyze the input stream, so be patient if you don't see input_info.json.

	// Create a subdirectory for video detection output.
	// The Yolo script (object detector) will be launched on the fly when new video segments 
	// in the lowest bitrate rendition are written to its output subdir and be found by the
	// file watcher. The Yolo model will be used to 
	// 1. Split video segments into frames, 
	// 2. Detect and mark objects in video frames, 
	// 3. Encode the marked frames into a single video segment,
	// 4. Output the marked video segments to the video detection output subdir,
	// The file watcher keeps watching the video detection output subdir, and upload the marked segments.
	/*if job.NeedObjectDetection(j) {
		detection_output_subdir := "video_detection"
		local_media_output_path_subdirs = append(local_media_output_path_subdirs, detection_output_subdir)
	}*/

	// Create local output paths. Shaka packager may have already created the paths.
	for _, sd := range local_media_output_path_subdirs {
		sd = local_media_output_path + sd
		_, err_fstat := os.Stat(sd)
		if errors.Is(err_fstat, os.ErrNotExist) {
			Log.Printf("Path %s does not exist. Creating it...\n", sd)
			err1 = os.Mkdir(sd, 0777)
			if err1 != nil {
				Log.Println("Failed to mkdir: ", sd, " Error: ", err1)
				os.Exit(1)
			}
		}
	}

	// Tell file watcher to also watches for detection output files
	var detection_target_bitrate string = undefined_bitrate
	if job.NeedObjectDetection(j) {
		detection_target_bitrate = j.Output.Detection.Input_video_bitrate
	}

	// Start a file watcher to check for new stream output from the packager and upload to remote origin server.
	var errWatchFiles error
	go func() {
		var watch_dirs []string
		watch_dirs = append(watch_dirs, local_media_output_path)
		for _, subdir := range local_media_output_path_subdirs {
			watch_dirs = append(watch_dirs, local_media_output_path+subdir+"/")
		}

		errWatchFiles = watchStreamFiles(j, watch_dirs, remote_media_output_path_base, ffmpegAlone, detection_target_bitrate)
	}()

	if errWatchFiles != nil {
		Log.Println("Failed to start file watcher. Error: ", errWatchFiles)
		// TODO: This is a critical error - Stream files will not be upload to remote origin server.
		//       Should worker_transcoder exit?
	}

	// Handle system signals to terminate worker_transcoder
	shutdown := make(chan os.Signal, 1)
	// syscall.SIGKILL cannot be handled
	signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-shutdown
		Log.Println("worker_transcoder shutting down!")

		// Received signal from worker_app:
		// - first, stop shaka packager and ffmpeg
		// - then, exit myself
		if !ffmpegAlone {
			processPackager, err3 := os.FindProcess(int(packagerCmd.Process.Pid))
			if err3 != nil {
				Log.Printf("Process id = %d (packagerCmd) not found. Error: %v\n", packagerCmd.Process.Pid, err3)
			} else {
				err3 = processPackager.Signal(syscall.Signal(syscall.SIGTERM))
				Log.Printf("process.Signal.SIGTERM on pid %d (Shaka packager) returned: %v\n", packagerCmd.Process.Pid, err3)
			}
		}

		processFfmpeg, err4 := os.FindProcess(int(ffmpegCmd.Process.Pid))
		if err4 != nil {
			Log.Printf("Process id = %d (ffmpeg) not found. Error: %v\n", ffmpegCmd.Process.Pid, err4)
		} else {
			err4 = processFfmpeg.Signal(syscall.Signal(syscall.SIGTERM))
			Log.Printf("process.Signal.SIGTERM on pid %d (ffmpeg) returned: %v\n", ffmpegCmd.Process.Pid, err4)
		}

		os.Exit(0)
	}()

	// Periodically manage ffmpeg and shaka packager
	d, _ := time.ParseDuration(transcoder_status_check_interval)
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func(ticker *time.Ticker) {
		for {
			select {
			case <-ticker.C:
				{
					if !ffmpegAlone {
						manageCommands(packagerCmd, ffmpegCmd)
					} else {
						manageFfmpegAlone(ffmpegCmd)
					}
				}
			case <-quit:
				{
					ticker.Stop()
					os.Exit(0)
				}
			}
		}
	}(ticker)

	// Periodically upload stream files
	d2, _ := time.ParseDuration(stream_file_upload_interval)
	ticker = time.NewTicker(d2)
	go func(ticker *time.Ticker) {
		for {
			select {
			case <-ticker.C:
				{
					uploadFiles()
				}
			}
		}
	}(ticker)

	// Periodically handle queued object detection jobs
	d3, _ := time.ParseDuration(process_detection_job_interval)
	ticker = time.NewTicker(d3)
	go func(ticker *time.Ticker) {
		for {
			select {
			case <-ticker.C:
				{
					executeNextDetectionJob()
				}
			}
		}
	}(ticker)

	set_upload_input_info_timer(local_media_output_path, remote_media_output_path_base)

	<-quit
}