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
)

type Upload_item struct {
	File_path                string
	Time_created             time.Time
	Num_retried              int
	Remote_media_output_path string
}

var Log *log.Logger
var upload_list *list.List
var init_segments_to_upload = make(map[string]Upload_item) // map key (string): rendition name; map value: Upload_item representing the init segments
var local_media_output_path string

const transcoder_status_check_interval = "2s"
const stream_file_upload_interval = "0.1s"
const upload_input_info_file_wait_time = "20s"
const max_upload_retries = 3
const num_concurrent_uploads = 5
var Detection_output_init_segment_path_local string

// The wait time from when a stream file is created by the packager, till when we are safe to upload the file (assuming the file is fully written)
const stream_file_write_delay_ms = 200

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
// - it is within the first "num_concurrent_uploads (3)" items in upload_list.
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
			if i > num_concurrent_uploads {
				Log.Printf("Current upload: %d > Max. uploads: %d. Let's upload later.\n", i, num_concurrent_uploads)
				break
			} else {
				Log.Printf("Current upload: %d < Max. uploads: %d. Proceed to upload.\n", i, num_concurrent_uploads)
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
	// FFmpeg media data segment template: "stream_%v/seg_%05d". The first segment looks like e.g., seg_00000.m4s.
	// Shaka packager template: "stream_%v/seg_$Number$.m4s". The first segment looks like e.g., seg_1.m4s.
	// TODO: remove dependency on data segment template.
	if isMediaDataSegment(local_file) && (strings.Contains(local_file, "seg_00000") || strings.Contains(local_file, "seg_1")) {
		item, ok := init_segments_to_upload[rendition_name[:len(rendition_name)-1]]
		if !ok {
			Log.Printf("Failed to find init segment item with rendition_name = %s\nAre you sure %s is a valid path and is the first media data segment?\n", rendition_name[:len(rendition_name)-1], local_file)
			return nil // This is NOT a fatal error, let's return nil.
		}

		Log.Printf("Add %s to UploadList\n", local_file)
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

func isMediaDataSegment(file_name string) bool {
	return (strings.Contains(file_name, ".mp4") ||
		strings.Contains(file_name, ".ts") ||
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

func isDetectionTarget(file_name string, detection_output_bitrate string) bool {
	return strings.Contains(file_name, detection_output_bitrate)
}

func isDetectionTargetTypeMediaDataSegment(file_name string, detection_output_bitrate string) bool {
	return strings.Contains(file_name, detection_output_bitrate) && isMediaDataSegment(file_name)
}

func isDetectionTargetTypeMediaInitSegment(file_name string, detection_output_bitrate string) bool {
	return strings.Contains(file_name, detection_output_bitrate) && isFmp4InitSegment(file_name)
}

func isDetectionTargetTypeHlsVariantPlaylist(file_name string, detection_output_bitrate string) bool {
	return strings.Contains(file_name, detection_output_bitrate) && isHlsVariantPlaylist(file_name)
}

func merge_init_and_data_segments(init_segment_path string, data_segment_path string) (string, error) {
	var merge_segment_path string = utils.Get_path_dir(init_segment_path) // merged_segments go to the same folder as init and data segments
	var merged_segment_buffer []byte
	var err error
	var bytes_init []byte
	var bytes_data []byte
	bytes_init, err = utils.Read_file(init_segment_path)
	if err != nil {
		Log.Printf("Failed to read detection output init segment: %s. Error: %v", init_segment_path, err)
		return merge_segment_path, err
	}

	bytes_data, err = utils.Read_file(data_segment_path)
	if err != nil {
		Log.Printf("Failed to read detection output data segment: %s. Error: %v", init_segment_path, err)
		return merge_segment_path, err
	}

	merged_segment_buffer = append(merged_segment_buffer, bytes_init...)
	merged_segment_buffer = append(merged_segment_buffer, bytes_data...)
	utils.Write_file(merged_segment_buffer, merge_segment_path)

	return merge_segment_path, nil
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
			Log.Printf("Failed to add %s to init segment tabele. Invalid rendition name: %s\n", file_path, rendition_name)
		}
	} else { // Add media data segments to upload_list
		Log.Printf("Add %s to UploadList\n", file_path)
		upload_list.PushBack(it)
	}
}

func run_detection(input_segment_path string) (string, error) {
	filename := utils.Get_path_filename(input_segment_path)
	filename = "od_" + filename // "od" stands for object detection
	output_segment_path := utils.Get_path_dir(input_segment_path) + filename

	Log.Printf("Run detection on input segment: %s. Output path: %s\n", input_segment_path, output_segment_path)
	return output_segment_path, nil
}

// https://github.com/fsnotify/fsnotify
func watchStreamFiles(watch_dirs []string, remote_media_output_path string, ffmpegAlone bool, detection_output_bitrate string) error {
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
							// The file is a stream file but a not detection target, upload it right away
							if !isDetectionTarget(event.Name, detection_output_bitrate) {
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
						if isDetectionTargetTypeMediaDataSegment(event.Name, detection_output_bitrate) {
							Log.Printf("Media data segment to detect: %s\n", event.Name)
							if Detection_output_init_segment_path_local == "" {
								Log.Printf("Error: data segment %s found, but init segment is missing\n", event.Name)
								continue
							}

							merged_segment_path, err := merge_init_and_data_segments(Detection_output_init_segment_path_local, event.Name)
							if err != nil {
								Log.Printf("Failed to merge detection output init and data segments. Error: %v\n", err)
								continue
							}

							// Run detection in a separate thread
							go func() {
								detection_output_segment_path, err := run_detection(merged_segment_path)
								if err != nil {
									Log.Printf("Failed to run detection on input = %s. Error: %v\n", merged_segment_path, err)
									// Replace with a slate segment?
									return
								}

								Log.Printf("Uploading detection output segment: %s\n", detection_output_segment_path)
								//addToUploadList(detection_output_segment_path, remote_media_output_path)
							}()
						} else if isDetectionTargetTypeMediaInitSegment(event.Name, detection_output_bitrate) { // The file is the init segment of the detection output, save the file path.
							Log.Printf("Media init segment to detect: %s", event.Name)
							Detection_output_init_segment_path_local = event.Name
						} else if isDetectionTargetTypeHlsVariantPlaylist(event.Name, detection_output_bitrate) { // The file is the variant playlist of the detection output, update it.
							Log.Printf("Hls variant playerlist to detect: %s", event.Name)
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

	// Tell file watch also watches for detection output files
	detection_output_bitrate := j.Output.Detection.Input_video_bitrate

	// Start a file watcher to check for new stream output from the packager and upload to remote origin server.
	var errWatchFiles error
	go func() {
		var watch_dirs []string
		watch_dirs = append(watch_dirs, local_media_output_path)
		for _, subdir := range local_media_output_path_subdirs {
			watch_dirs = append(watch_dirs, local_media_output_path+subdir+"/")
		}

		errWatchFiles = watchStreamFiles(watch_dirs, remote_media_output_path_base, ffmpegAlone, detection_output_bitrate)
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
					// Periodically call function uploadFiles every "stream_file_upload_interval" time units
					uploadFiles()
				}
			}
		}
	}(ticker)

	set_upload_input_info_timer(local_media_output_path, remote_media_output_path_base)

	<-quit
}