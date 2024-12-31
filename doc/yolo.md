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

The API server validates the detection configuration before it creates the live job. If any field is missing, its default value will be used. 

## Limitations
1. Only H.264 (libx264) and H.265 (libx265) is allowed to be the codec format for re-encoding and converting annotated images to live video segments, AV1 is not supported.