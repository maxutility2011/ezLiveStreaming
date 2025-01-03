# live real-time object detection in ezLiveStreaming
ezLiveStreaming enables real-time object detection, allowing for the identification of objects in live video streams. This document offers a detailed overview of its design.

## Overview
ezLiveStreaming uses YOLOv8 (You Only Look Once) machine learning model for detecting objects in live video streams. Users can add object detection configuration in their live job requests to request a separate detected (annotated) video output in addition to the main transcoded video outputs. The detection configuration allows specification of video resolution, frame rate and other encoder settings of the annotated video output. The annotated output is packaged as an HLS stream which can be streamed just like the main outputs. A separate HLS variant playlist url is returned to the users in the demo UI.

When the API server receives a live job with detection enabled, it first validates the detection configuration. If the configuration is invalid, warnings will be returned or the live job is rejected in case of fatal errors. If the configuration is valid, the API server passes the job to the job scheduler which assigns the job to a live worker with proper computing power. Due to high computation demand of ML inference and live video transcoding, the assigned live worker must be powerful enough to run real-time Yolo inference (object detection). Delay in Yolo inference can cause interruption in streaming the live annotated video. A compute-optimized AWS instance such as c5.2xlarge is recommended to be the live worker for object detection.

On a live worker, the worker_transcoder process is responsible for allocating resources for object detection as well as coordinating various pre-detection, detection and post-detection tasks. Worker_transcoder adds a separate encoding thread (within FFmpeg transcoder) and packaging thread (within Shaka packager) for generating the **detection target output**. **Detection target output** is the dedicated transcoder/packager output just for object detection. It is the input to the object detector. For every video segment (e.g., a fmp4 video segment) in the detection target output, worker_transcoder launches a separate detector process to pre-process, run detection and output an annotated video segment. This annotated video segment is then uploaded to the cloud storage for delivery. Worker_transcoder also maintains and updates the HLS variant playlist for live-streaming the annotated video to end users.

The Yolo inference procedure is implemented by Python and shell scripts in the github repo [od_yolov8](https://github.com/maxutility2011/od_yolov8). The scripts take one self-initialized video segment as input, break the video segment into a sequence of images, run object detection on the images, then re-encode the annotated images to an annotated video segment. 

Worker_transcoder reads the annotated video segment, save the initialization section (**MOOV**) as init segment, and the media data section (**MOOF** and **MDAT**) as video data segment, then upload them to cloud storage. Also, whenever a video data segment is uploaded, worker_transcoder updates the HLS playlist to add that segment for live streaming. 

Due to high computation demand, video resolutions and frame rates of detection target outputs must be limited to avoid long inference time and hence interruption of the live annotated stream. On the other side, video resolution can't be set too low as doing so may have a negative impact on object detection accuracy.

## Configuration of object detection in live jobs
Object detection can be configured by adding a "Detection" block under "Output". "Detection" is parallel to "Video_outputs" which are both subblocks under "Output".
```
{
  "Output": {
    "Stream_type": "hls",
    "Segment_format": "fmp4",
    "Fragment_duration": 5,
    "Segment_duration": 10,
    "Low_latency_mode": 0,
    "Time_shift_buffer_depth": 120,
    "Detection": {
      "Input_video_frame_rate": 10,
      "Input_video_resolution_height": 180,
      "Input_video_resolution_width": 320,
      "Input_video_bitrate": "150k",
      "Input_video_max_bitrate": "250k",
      "Input_video_buf_size": "250k",
      "Encode_codec": "h264",
      "Encode_preset": "veryfast",
      "Encode_crf": 25
    }
    ......
  }
}
```
The "Detection" block is divided into two sections. 

Any fields with "Input_" prefix are used by the transcoder/packager to generate the **detection target output**. The prefix "Input_" indicates these configuration fields are for generating the **input** to the object detector.
| Field | Definition | Data type | Comments | Default value |
| --- | --- | --- | --- | --- |
| Input_video_frame_rate | Frame rate of video inputs to the object detector | integer | Object detector will break each input video segment to **fr** number of images where **fr** is the input video frame rate. | 25 |
| Input_video_resolution_height | Height (pixels) of the video input to the detector | integer | | 320 |
| Input_video_resolution_width | Width (pixels) of the video input to the detector | integer | | 180 |
| Input_video_bitrate | Bitrate used by the transcoder when generating the video input to the detector | string | | "150k" |
| Input_video_max_bitrate | Max bitrate used by the transcoder when generating the video input to the detector | string | | "250k" |
| Input_video_buf_size | VBV buffer size used by the transcoder when generating the video input to the detector | string | | "250k" |

Note that the transcoder only uses H.264 and H.265 for transcoding the **detection target output**. AV1 will never be used.

All other fields are for re-encoding the annotated images to a single output video segment. 
| Field | Definition | Data type | Comments | Default value |
| --- | --- | --- | --- | --- |
| Encode_codec | Codec used by the video re-encoder | string | Only H.264 and H.265 is allowed | "h264" |
| Encode_preset | Preset used by the video re-encoder | string | "veryfast" | 
| Encode_crf | CRF used by video re-encoder | integer | 25 | 

The API server validates the detection configuration before it creates the live job. If any field is missing, its default value are used, but warnings are also returned to indicate the missing fields.  

## Pre-detection processing
When a live job with detection enabled (called **live detection job**) is launched on a worker, a worker_transcoder process is created to handle object detection and live transcoding. The worker_transcoder first checks to see if detection is enabled. If so, worker_transcoder combines the detection target output with the list of main video outputs so that the transcoder (FFmpeg) and packager (Shaka packager) generates the configured output for the consumption of object detector. Worker_transcoder also adds the subfolder holding the detection target output data (HLS playlist and media segments) to its watch folder list. When new segments and playlist are written/updated under that subfolder, worker_transcoder is notified and a sequence of media processing and object detection will be triggered. 

When worker_transcoder's file watcher finds HLS master/variant playlist or media segments in any main video/audio output rendition, the new file is uploaded to cloud storage right away. If the file is a variant playlist (variant playlist is updated constantly during a live streaming event), its local storage path is cached for later use. The playlist can NOT be uploaded to cloud storage right away, because the media segments towards to the end of the playlist (the latest few segments) have not gone through the object detector and are not annotated yet. Only when a video segment finishes object detection and gets uploaded to cloud storage, it is added to the variant playlist. Then the playlist can be uploaded and the newly detected segment can be made available for live streaming. 

If the file watcher finds a detection target video initialization segment (**MOOV** only), it caches the local storage path of the init segment. Commonly, the content of init segments do not change during a live event unless video encoder reconfiguration happens (which is not allowed in ezLiveStreaming). 

If the file watcher finds a detection target video data segment (**MOOF** and **MDAT**), the file watcher loads the init segment and concatenate with the video data segment. The concatenation outputs a self-initialized video segment with both the initialization section and data section. The concatenation step is needed because the object detector requires self-initialized video segments as inputs. Specifically, the object detector converts a video segment into a sequence of images. The segment-to-images conversion requires the initialization section. Next, a new object detection job is created and inserted into the detection job queue. Worker_transcoder uses a separate monitor thread to fetch detection jobs from the queue and execute the job (i.e., run object detection on the input video segment). 

## Object detection
The detection job monitor thread (started from the worker_transcoder main thread) periodically polls the queue for new detection jobs. The thread only executes the job if the number of currently running detection jobs does not exceed a hardcoded maximum allowed number. Limiting the number of running detection jobs can prevent overloading the live worker since Yolo inference is very compute-intensive. The execution of a  detection job runs in its own separate thread (detector thread), so it does not block the detection job monitor thread.

The object detection scripts reside in a different Github repo, https://github.com/maxutility2011/od_yolov8. [od.sh](https://github.com/maxutility2011/od_yolov8/blob/main/od.sh) is the main script called by worker_transcoder when executing a detection job. It contains 3 major steps. First, run FFmpeg to convert the input video file to a sequence of images. Second, run Yolo inference ([yolo.py](https://github.com/maxutility2011/od_yolov8/blob/main/yolo.py)) on the sequence of images to generate a sequence of detected/annotated images. Third, run FFmpeg to merge the annotated images to a single output video file. 

### Input video to image conversion
The input video file (i.e., a self-initialized media segment) is broken into **Fr * Dur** images using FFmpeg. **Fr** is given by the **Input_video_frame_rate** in the "Detection" configuration block of the live job. **Dur** is the duration of the input video. For example, if Input_video_frame_rate is 10 fps and input video duration is 10 secs, the 10 secs input video will be broken into 100 images. 

### Yolo inference
The Yolo inference script, [yolo.py](https://github.com/maxutility2011/od_yolov8/blob/main/yolo.py) reads the images from the input folder, runs inference on each image one by one, and writes the annotated images to the output folder. Yolov8 is used for the inference. All the prerequisites by Yolov8 (e.g., pytorch, ultralytics, Yolo model file) are pre-installed in the docker image.

On my c5.2xlarge instance without GPU acceleration, inference of one 320x180 image takes about 50ms. That means the input video frame rate cannot be higher than 20 fps, otherwise we won't be able to infer/detect the whole segment in real-time. In practice, the loading and initialization of the Yolo model also takes 1-2 secs, so the actual max. possible input video frame rate is less than 20 fps. 

For each input media segment file, a subfolder is created to hold the intermediate files and log files, e.g., input images, output images, Yolo script log files, FFmpeg re-encoder's log file. For example, od.sh creates subfolder *seg_1* under */tmp/output_efa8d130-a969-4139-b899-afa16115c473/video_150k/* when processing input video file, *seg_1.mp4*. The input images and output images are held under */tmp/output_efa8d130-a969-4139-b899-afa16115c473/video_150k/inputs and */tmp/output_efa8d130-a969-4139-b899-afa16115c473/video_150k/outputs*.

![screenshot](doc/diagrams/yolo_working_dir.png)

### Re-encoding
Next, the script od.sh uses FFmpeg to merge and re-encode the annotated output images into a single output video file in fragmented MP4 format. The output video is the annotated video segment. The MP4 video segment contains MOOV, SIDX, MOOF and MDAT box. Another argument to FFmpeg is the video timescale used by the original input video segment. We will cover this in the next section.

## Post-detection processing
The output annotated video segments by od.sh are self-initialized mp4 files. To comply with HLS-fmp4 standard, we need to strip off the initialization section and save only the media data section. Specifically, we need to save the initialization section (**MOOV** box) as the initialization segment, e.g., "segment_init.mp4", and save the media data section (**SIDX**, **MOOF** and **MDAT** boxes) as media data segment, e.g., "segment_1.m4s". When a detector thread finish detection inference, it continues job execution to convert the annotated mp4 segment to a fragmented mp4 segment. The open-source video processing software **mp4box** by GPAC is used for this purpose. Specifically, **mp4box** converts the mp4 input to an HLS stream with only one single fragmented video data segment in m4s format, and one video initialization segment. The m4s segment only contains SIDX, MOOF and MDAT boxes. The duration of **mp4box**'s output video data segment is the same as the original input media segment. The initialization segment contains only the MOOV box. **mp4box** also outputs a dummy HLS m3u8 file which is NOT used anywhere in the system.

The output fmp4 segment from **mp4box** still cannot be streamed using HLS due to a non-continuous timestamps across segments. When the FFmpeg re-encoder converts annotated images to mp4 video output, the timestamp of the re-encoded video frames always starts from 0. This will cause timestamp reset every **D** second where **D** is the duration of media segments. In fact, the segment timestamp shall start from 0 when the very first frame in a live channel is generated (at the time when the live feed to the channel is initially ingested), and the timestamp shall increment monotonically throughout the lifetime of the channel. As a result of discrete segment timestamp, HLS playback will behave unexpectedly or even stops completely. 

To fix the timestamp issue, we need to re-timestamp the fmp4 segments to make their timestamps continuous and monotonically increasing. Specifically, we need to extract and cache the timestamp of the original video segments, as well as the **timescale** (the time resolution used for representing the timestamps) at the time of pre-detection processing. Next, after detection completes, we need to re-inject the original timestamp and timescale values into the output fmp4 video segments. Because the object detection process does not change the duration of a video segment, the original timestamps can be re-used by the annotated segments and they should still be continuous and monotonically increasing. Keeping the original timestamps allows the output fmp4 segments to be continuously playable in an HLS stream.

In a fmp4 segment, the timestamp of a video segment is stored in **BaseMediaDecodeTime** field of the **TRAF** box (inside **MOOF** box). The timescale is stored in the **SIDX** box. To extract and re-injects timestamps and timescales, I wrote a utility library, [media_utils](https://github.com/maxutility2011/media_utils). 

Finally, we need to update the HLS master playlist and generate the variant playlist for the annotated video rendition. First, the original master playlist contains the detection target rendition which should be removed. The Shaka packager includes detection target rendition in the master playlist because it cannot distinguish main output renditions and detection target output renditions. We cannot leave detection target output rendition in the master playlist because that rendition with annotated video should not considered as a candidate for HLS Adaptive BitRate (ABR) switching. Next, the original variant playlist of the detection target output rendition cannot be re-used to play the annotated video segments. For one thing, the video segments are renamed after all the detection steps. The old segment URLs have to be replaced by the new URLs. Secondly, the latest segments in the original variant playlist should not be published to video players right away since they may still be under processing of the object detector. Instead, an annotated video segment shall only be added to the playlist and published to video players after they finish all the detection processing and be uploaded to cloud storage. So, the very last thing to do by a detector thread is to update the annotated variant playlist and upload the playlist to cloud storage. This happens to every annotated video segment.

## Limitations
1. Only H.264 (libx264) and H.265 (libx265) is allowed to be the codec format for re-encoding and converting annotated images to live video segments, AV1 is not supported.