var init_playback_url = "https://livesim.dashif.org/livesim/testpic_2s/Manifest.mpd"
var playback_url = init_playback_url;
var detection_playlist_url = "";
var ingest_url = "";
var create_button;
var stop_button;
var resume_button;
var show_button;
var play_button;
var response_code;
var response_body;
var job_request;
var video;
var job_id;
var listJobsTimer = null;
const listJobsInterval = 2000;
var detection_enabled = false;
var detection_video_bitrate = ""

// Download sample jobs from the server.
var sample_live_job = '';
var url1 = "http://" + location.host + "/specs/sample_live_job.json";
$.getJSON(url1, function(json) {
  sample_live_job = JSON.stringify(json, null, 2);
});

var sample_live_job_without_drm = '';
var url2 = "http://" + location.host + "/specs/sample_live_job_without_drm.json";
$.getJSON(url2, function(json) {
  sample_live_job_without_drm = JSON.stringify(json, null, 2);
});

var sample_live_job_av1 = '';
var url3 = "http://" + location.host + "/specs/sample_live_job_av1.json";
$.getJSON(url3, function(json) {
  sample_live_job_av1 = JSON.stringify(json, null, 2);
});

var sample_live_job_object_detection = '';
var url4 = "http://" + location.host + "/specs/sample_live_job_object_detection.json";
$.getJSON(url4, function(json) {
  sample_live_job_object_detection = JSON.stringify(json, null, 2);
});

async function initPlayer() {
    // Create a Player instance.
    const video = document.getElementById('video');
  
    const player = new shaka.Player(video);
  
    player.configure('streaming.lowLatencyMode', true);
    player.configure('streaming.autoLowLatencyMode', true);
  
    // Attach player to the window to make it easy to access in the JS console.
    window.player = player;
  
    // Try to load a manifest.
    // This is an asynchronous process.
    try {
      await player.load(playback_url);
      // This runs if the asynchronous load is successful.
      console.log('The video has now been loaded!');
    } catch (e) {
      onError(e);
    }
}

function changeJob() {
 	var job_list = document.getElementById("liveJobList");
	var job_request = document.getElementById("job_request");
	if (job_list.options[job_list.selectedIndex].text == "hls live with clear-key drm") {
		  j = JSON.parse(sample_live_job)
		  job_request.innerHTML = JSON.stringify(j, null, 2)
	} else if (job_list.options[job_list.selectedIndex].text == "hls live without drm") {
		  j = JSON.parse(sample_live_job_without_drm)
      job_request.innerHTML = JSON.stringify(j, null, 2)
	} else if (job_list.options[job_list.selectedIndex].text == "hls live with av1 codec") {
      j = JSON.parse(sample_live_job_av1)
      job_request.innerHTML = JSON.stringify(j, null, 2)
  } else if (job_list.options[job_list.selectedIndex].text == "hls live with Yolo object detection") {
      j = JSON.parse(sample_live_job_object_detection)
      job_request.innerHTML = JSON.stringify(j, null, 2)
  }
}

function onError(e) {
    window.alert("Playback error: ", e);
}

async function reloadPlayer()
{
  try {
    await window.player.load(playback_url);
    // This runs if the asynchronous load is successful.
    console.log('The video has now been loaded!');
  } catch (e) {
    // onError is executed if the asynchronous load fails.
    onError(e);
  }
}

function initApp() {
    // Install built-in polyfills to patch browser incompatibilities.
    shaka.polyfill.installAll();
  
    // Check to see if the browser supports the basic APIs Shaka needs.
    if (shaka.Player.isBrowserSupported()) {
      // Everything looks good!
      initPlayer();
    } else {
      // This browser does not have the minimum set of APIs we need.
      console.error('Browser not supported!');
    }
}

window.addEventListener("DOMContentLoaded", (event) => {
    initApp()

    create_button = document.getElementById('create');
    create_button.addEventListener('click', (event) => {
        createJob();
    });
    
    stop_button = document.getElementById('stop');
    stop_button.disabled = true
    stop_button.addEventListener('click', (event) => {
        stopJob();
    });
    
    resume_button = document.getElementById('resume');
    resume_button.disabled = true
    resume_button.addEventListener('click', (event) => {
        resumeJob();
    });

    delete_button = document.getElementById('delete');
    delete_button.disabled = true
    delete_button.addEventListener('click', (event) => {
        deleteJob();
    });

    show_button = document.getElementById('show');
    show_button.disabled = true
    show_button.addEventListener('click', (event) => {
        showJob();
    });

    play_button = document.getElementById('play');
    play_button.addEventListener('click', (event) => {
        playVideo();
    });

    response_code = document.getElementById('response_code');

    response_body = document.getElementById('response_body');

    job_essentials = document.getElementById('job_essentials');
    
    job_request = document.getElementById('job_request');
    
    job_essentials.innerHTML = "Playback URL and RTMP ingest URL will be shown after clicking the Create button. Please push your live feed to the RTMP ingest URL. After you start feeding the live channel, wait 15 secs then hit the Play button to play your channel."
    video = document.getElementById('video');
});
    
var hasAv1 = false;
function playVideo() {
  if (!hasAv1) {
    reloadPlayer()
  } else {
    let msg = "The built-in player does not support AV1 codec. Please use https://hlsjs.video-dev.org/demo/";
    window.alert(msg);
  }
}

var showJobTimer = null;
var playbackTimer = null;

function startShowJobTimer() {
    showJobTimer = setTimeout(showJob, 1000);
}

function startPlaybackTimer() {
    playbackTimer = setTimeout(playVideo, 16000);
}

function showJob() {
    showJobTimer = setTimeout(showJob, 5000);

    // Get Job state
    let show_job_url = api_server_url + "jobs/";
    show_job_url += job_id;
    let show_job_req = new XMLHttpRequest();
    show_job_req.open("GET", show_job_url, true);

    show_job_req.onload = function (e) {
        if (show_job_req.readyState === show_job_req.DONE) {
          if (show_job_req.status === 200) {
            let job_resp = show_job_req.response;
            let j = JSON.parse(job_resp);
            job_id = j.Id;

            playback_url = j.Playback_url;

            let je = {};
            je.playback_url = j.Playback_url;
            if (detection_enabled) {
              je.detection_playlist_url = detection_playlist_url;
            }
            
            je.rtmp_ingest_url = j.RtmpIngestUrl;
            je.drm_key_id = j.DrmEncryptionKeyInfo.Key_id;
            je.drm_key = j.DrmEncryptionKeyInfo.Key;
            je.job_state = j.State;
            je.validation_warnings = j.Job_validation_warnings;
            je.ingress_bandwidth_kbps = j.Ingress_bandwidth_kbps; 
            je.transcoding_cpu_utilization = j.Transcoding_cpu_utilization;  
            je.input_info = j.Input_info_url;

            job_essentials.innerHTML = JSON.stringify(je, null, 2);
            response_code.innerHTML = "status code=" + show_job_req.status;
            response_body.innerHTML = JSON.stringify(j, null, 2);

            if (j.Spec.Output.Detection.Input_video_bitrate) {
              detection_enabled = true;
              detection_video_bitrate = j.Spec.Output.Detection.Input_video_bitrate;
            }
          } else {
            let job_resp = this.response;
            window.alert(job_resp);
          }
        }
    }
    
    show_job_req.send();

    if (detection_enabled) {
      const url = new URL(playback_url);
      const baseUrlPathname = url.pathname.substring(0, url.pathname.lastIndexOf('/'));
      const baseUrl = `${url.origin}${baseUrlPathname}`;

      detection_playlist_url = baseUrl + "/video_" + detection_video_bitrate + "/playlist_detected.m3u8";
    }
}

function createJob() {
    let create_job_url = api_server_url + "jobs";
    let create_job_req = new XMLHttpRequest();
    create_job_req.open("POST", create_job_url, true);
    create_job_req.setRequestHeader("Content-Type", "application/json");

    create_job_req.onload = function (e) {
        if (create_job_req.readyState === create_job_req.DONE) {
          if (create_job_req.status === 201) {
            let job_resp = this.response;
            let j = JSON.parse(job_resp);
            job_id = j.Id;
            response_code.innerHTML = "status code=" + create_job_req.status;
            response_body.innerHTML = JSON.stringify(j, null, 2);
            create_button.disabled = true;
            stop_button.disabled = false;
            resume_button.disabled = false;
            show_button.disabled = false;

            startShowJobTimer();
          } else {
            let job_resp = this.response;
            window.alert(job_resp);
          }
        }
    }

    let job_body = ""
    if (job_request.value != "") {
        job_body = job_request.value;
        try {
            j = JSON.parse(job_body)
            j.Output.Video_outputs.forEach((o) => {
              if (o.Codec == "av1") {
                hasAv1 = true;
              }
            })
        } catch (e) {
            window.alert("Invalid JSON")
            return
        }
    } else {
        j = JSON.parse(sample_live_job)
        j.Output.Video_outputs.forEach((o) => {
          if (o.Codec == "av1") {
            hasAv1 = true;
          }
        })

        job_body = JSON.stringify(j)
        job_request.innerHTML = JSON.stringify(j, null, 2)
    }
    
    create_job_req.send(job_body);
    time_job_created = Date.now()

}

function cleanup() {
  //stopLiveFeed()
  stopJob()
}

window.onbeforeunload = cleanup;

function stopJob() {
    let stop_job_url = api_server_url + "jobs/";
    stop_job_url += job_id
    stop_job_url += "/stop"

    let stop_job_req = new XMLHttpRequest();
    stop_job_req.open("PUT", stop_job_url, true);

    stop_job_req.onload = function (e) {
        if (stop_job_req.readyState === stop_job_req.DONE) {
          if (stop_job_req.status === 202) {
            response_code.innerHTML = "status code=" + stop_job_req.status
            delete_button.disabled = false;
            //response_body.innerHTML = JSON.stringify(JSON.parse(job_resp), null, 2)
          } else {
            let job_resp = this.response;
            window.alert(job_resp);
          }
        }
    }
    
    stop_job_req.send();
}

function resumeJob() {
    let resume_job_url = api_server_url + "jobs/";
    resume_job_url += job_id
    resume_job_url += "/resume"

    let resume_job_req = new XMLHttpRequest();
    resume_job_req.open("PUT", resume_job_url, true);

    resume_job_req.onload = function (e) {
        if (resume_job_req.readyState === resume_job_req.DONE) {
          if (resume_job_req.status === 202) {
            response_code.innerHTML = "status code=" + resume_job_req.status
            startShowJobTimer()
          } else {
            let job_resp = this.response;
            window.alert(job_resp);
          }
        }
    }
    
    resume_job_req.send();
}

function deleteJob() {
  let delete_job_url = api_server_url + "jobs/";
  delete_job_url += job_id

  let delete_job_req = new XMLHttpRequest();
  delete_job_req.open("DELETE", delete_job_url, true);

  delete_job_req.onload = function (e) {
      if (delete_job_req.readyState === delete_job_req.DONE) {
        if (delete_job_req.status === 202) {
          response_code.innerHTML = "status code=" + delete_job_req.status
          startShowJobTimer()
        } else {
          let job_resp = this.response;
          window.alert(job_resp);
        }
      }
  }
  
  delete_job_req.send();
}