var playback_url = "http://localhost:4080/ezliveStreaming/1.mp4";
var ingest_url = ""
var create_button;
var stop_button;
var resume_button;
var show_button;
var response_code;
var response_body;
var job_request;
var video;
var job_id;
var listJobsTimer = null;
var listJobsInterval = 2000

async function initPlayer() {
    //var urlInput = document.getElementById('url');
  
    // Create a Player instance.
    const video = document.getElementById('video');
  
    /*video.addEventListener('loadstart', (event) => {
        loadStartTime = Date.now();
    });
  
    video.addEventListener('loadeddata', (event) => {
        loadEndTime = Date.now();
    });
  
    video.addEventListener('waiting', (event) => {
      ++waitingEvents;
    });*/
  
    const player = new shaka.Player(video);
  
    player.configure('streaming.lowLatencyMode', true);
    player.configure('streaming.autoLowLatencyMode', true);
  
    // Attach player to the window to make it easy to access in the JS console.
    window.player = player;
  
    // Listen for error events.
    player.addEventListener('error', onErrorEvent);
  
    // Try to load a manifest.
    // This is an asynchronous process.
    try {
      await player.load(playback_url);
      // This runs if the asynchronous load is successful.
      console.log('The video has now been loaded!');
    } catch (e) {
      // onError is executed if the asynchronous load fails.
      onError(e);
    }
  
    //startStatsTimer(); 
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
  
    /*var urlInput = document.getElementById('url');
    urlInput.addEventListener('change', (event) => {
          manifestUri = event.target.value;
    });
  
    var urlButton = document.getElementById('load-url');
    urlButton.addEventListener('click', (event) => {
      reloadPlayer();
    });*/
}

window.addEventListener("DOMContentLoaded", (event) => {
    initApp()

    create_button = document.getElementById('create');
    create_button.addEventListener('click', (event) => {
        createJob();
    });
    
    stop_button = document.getElementById('stop');
    stop_button.addEventListener('click', (event) => {
        stopJob();
    });
    
    resume_button = document.getElementById('resume');
    resume_button.addEventListener('click', (event) => {
        resumeJob();
    });

    show_button = document.getElementById('show');
    show_button.addEventListener('click', (event) => {
        showJob();
    });

    show_button = document.getElementById('play');
    show_button.addEventListener('click', (event) => {
        playVideo();
    });

    response_code = document.getElementById('response_code');

    response_body = document.getElementById('response_body');

    job_essentials = document.getElementById('job_essentials');
    
    job_request = document.getElementById('job_request');
    
    video = document.getElementById('video');
});
    
/*
var cfg = 
{
    "debug": false,
    "enableWorker": true,
    "lowLatencyMode": true,
    "backBufferLength": 90,
    "maxLiveSyncPlaybackRate": 1,
    "liveSyncDuration": 6,
    "liveMaxLatencyDuration": 7
};

if (Hls.isSupported()) {
    var aurora_stats_hls = new Hls(cfg);
    aurora_stats_hls.attachMedia(video);
    aurora_stats_hls.loadSource(playback_url);
} else {
    window.alert("HLS not supported");
}
*/

function playVideo() {
    reloadPlayer()
}

var showJobTimer = null;
var playbackTimer = null;

function startShowJobTimer() {
    window.alert("show job!");
    showJobTimer = setTimeout(showJob, 1000);
}

/*function startPlaybackTimer() {
    playbackTimer = setTimeout(showJob, 16000);
}*/

function showJob() {
    let show_job_url = "http://localhost:1080/jobs/";
    show_job_url += job_id;
    let show_job_req = new XMLHttpRequest();
    show_job_req.open("GET", show_job_url, true);

    show_job_req.onload = function (e) {
        if (show_job_req.readyState === show_job_req.DONE) {
          if (show_job_req.status === 200) {
            let job_resp = show_job_req.response;
            let j = JSON.parse(job_resp)
            job_id = j.Id;

            playback_url = j.Playback_url;
            ingest_url = j.RtmpIngestUrl;

            let je = {}
            je.playback_url = playback_url
            je.rtmp_ingest_url = ingest_url

            job_essentials.innerHTML = JSON.stringify(je, null, 2)
            response_code.innerHTML = "status code=" + show_job_req.status
            response_body.innerHTML = JSON.stringify(j, null, 2)

            //startPlaybackTimer()
            //window.alert(job_resp);
          } else {
            console.log("Show live job failed. Status code:" + show_job_req.status);
          }
        }
    }
    
    show_job_req.send();
}

function createJob() {
    let create_job_url = "http://localhost:1080/jobs";
    let create_job_req = new XMLHttpRequest();
    create_job_req.open("POST", create_job_url, true);
    create_job_req.setRequestHeader("Content-Type", "application/json");

    create_job_req.onload = function (e) {
        if (create_job_req.readyState === create_job_req.DONE) {
          if (create_job_req.status === 201) {
            let job_resp = this.response;
            let j = JSON.parse(job_resp)
            job_id = j.Id
            response_code.innerHTML = "status code=" + create_job_req.status
            response_body.innerHTML = JSON.stringify(j, null, 2)
            create_button.disabled = true

            startShowJobTimer()
            //window.alert(job_resp);
          } else {
            console.log("create new live job failed. Status code:" + create_job_req.status);
          }
        }
    }

    let job_body = ""
    if (job_request.value != "") {
        job_body = job_request.value;
    }
    
    create_job_req.send(job_body);
    //hls.loadSource(manifestUri_aurora_stats);
}

function cleanup()
{
  stopJob()
}

window.onbeforeunload = cleanup;

function stopJob() {
    let stop_job_url = "http://localhost:1080/jobs/";
    stop_job_url += job_id
    stop_job_url += "/stop"

    let stop_job_req = new XMLHttpRequest();
    stop_job_req.open("PUT", stop_job_url, true);

    stop_job_req.onload = function (e) {
        if (stop_job_req.readyState === stop_job_req.DONE) {
          if (stop_job_req.status === 202) {
            let job_resp = this.response;
            response_code.innerHTML = "status code=" + stop_job_req.status
            //response_body.innerHTML = JSON.stringify(JSON.parse(job_resp), null, 2)
            //window.alert(job_resp);
          } else {
            console.log("stop new live job failed. Status code:" + stop_job_req.status);
          }
        }
    }
    
    stop_job_req.send();
}

function resumeJob() {
    let resume_job_url = "http://localhost:1080/jobs/";
    resume_job_url += job_id
    resume_job_url += "/resume"

    let resume_job_req = new XMLHttpRequest();
    resume_job_req.open("PUT", resume_job_url, true);

    resume_job_req.onload = function (e) {
        if (resume_job_req.readyState === resume_job_req.DONE) {
          if (resume_job_req.status === 202) {
            let job_resp = this.response;
            response_code.innerHTML = "status code=" + resume_job_req.status
            //response_body.innerHTML = JSON.stringify(JSON.parse(job_resp), null, 2)
            startShowJobTimer()
          } else {
            console.log("stop new live job failed. Status code:" + resume_job_req.status);
          }
        }
    }
    
    resume_job_req.send();
}