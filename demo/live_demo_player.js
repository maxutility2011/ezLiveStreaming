var api_server_url = "http://ec2-34-202-195-77.compute-1.amazonaws.com:1080/";
var playback_url = "https://livesim.dashif.org/livesim/testpic_2s/Manifest.mpd";
var ingest_url = "";
var create_button;
var stop_button;
var resume_button;
var show_button;
var livefeed_button;
var stoplivefeed_button;
var response_code;
var response_body;
var job_request;
var video;
var job_id;
var listJobsTimer = null;
var listJobsInterval = 2000
var sample_live_job = '{"Output": {"Stream_type": "dash","Segment_format": "fmp4","Segment_duration": 4,"Low_latency_mode": false,"Video_outputs": [{"Label": "video365k","Codec": "h264","Framerate": 25,"Width": 640,"Height": 360,"Bitrate": "365k","Max_bitrate": "500k","Buf_size": "500k","Preset": "faster","Threads": 2,"Gop_size": 2},{"Label": "video550k","Codec": "h264","Framerate": 25,"Width": 768,"Height": 432,"Bitrate": "550k","Max_bitrate": "750k","Buf_size": "750k","Preset": "faster","Threads": 2,"Gop_size": 2}],"Audio_outputs": [{"Codec": "aac","Bitrate": "128k"}]}}'
var isLivefeeding = false

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

	/*
    livefeed_button = document.getElementById('livefeed');
    livefeed_button.addEventListener('click', (event) => {
        liveFeed();
    });

    stoplivefeed_button = document.getElementById('stoplivefeed');
    stoplivefeed_button.addEventListener('click', (event) => {
        stopLiveFeed();
    });
	*/

    show_button = document.getElementById('play');
    show_button.addEventListener('click', (event) => {
        playVideo();
    });

    response_code = document.getElementById('response_code');

    response_body = document.getElementById('response_body');

    job_essentials = document.getElementById('job_essentials');
    
    job_request = document.getElementById('job_request');
    
    job_essentials.innerHTML = "Playback URL and RTMP ingest URL will be shown after clicking the Create button. Please push your live feed to the RTMP ingest URL. After you start feeding the live channel, wait 15 secs then hit the Play button to play your channel."
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
    var stats_hls = new Hls(cfg);
    stats_hls.attachMedia(video);
    stats_hls.loadSource(playback_url);
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
    showJobTimer = setTimeout(showJob, 1000);
}

function startPlaybackTimer() {
    playbackTimer = setTimeout(playVideo, 16000);
}

function startLiveFeedTimer() {
    showJobTimer = setTimeout(liveFeed, 500);
}

function liveFeed() {
    let live_feed_url = api_server_url + "feed";
    let live_feed_req = new XMLHttpRequest();
    live_feed_req.open("POST", live_feed_url, true);
    live_feed_req.setRequestHeader("Content-Type", "application/json");

    live_feed_req.onload = function (e) {
        if (live_feed_req.readyState === live_feed_req.DONE) {
          if (live_feed_req.status === 201) {
            response_code.innerHTML = "status code=" + live_feed_req.status
            livefeed_button.disabled = true
            isLivefeeding = true
            startPlaybackTimer()
          } else {
            console.log("create new live feed failed. Status code:" + create_job_req.status);
          }
        }
    }

    let feed_body = ""
    if (ingest_url != "") {
        body = {}
        body.RtmpIngestUrl = ingest_url
        feed_body = JSON.stringify(body)
        live_feed_req.send(feed_body);
    } else {
        console.log("create new live feed failed.")
        return
    }
}

function stopLiveFeed() {
    if (!isLivefeeding) {
        return
    }

    let stop_live_feed_url = api_server_url + "feed";
    let stop_live_feed_req = new XMLHttpRequest();
    stop_live_feed_req.open("DELETE", stop_live_feed_url, true);

    stop_live_feed_req.onload = function (e) {
        if (stop_live_feed_req.readyState === stop_live_feed_req.DONE) {
          if (stop_live_feed_req.status === 201) {
            response_code.innerHTML = "status code=" + stop_live_feed_req.status
            livefeed_button.disabled = false
          } else {
            console.log("stop live feed failed. Status code:" + stop_live_feed_req.status);
          }
        }
    }
    
    stop_live_feed_req.send();
}

function showJob() {
    let show_job_url = api_server_url + "jobs/";
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

            //startLiveFeedTimer()
            //window.alert(job_resp);
          } else {
            console.log("Show live job failed. Status code:" + show_job_req.status);
          }
        }
    }
    
    show_job_req.send();
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
        try {
            JSON.parse(job_body)
        } catch (e) {
            window.alert("Invalid JSON")
            return
        }
    } else {
        j = JSON.parse(sample_live_job)
        job_body = JSON.stringify(j)
        job_request.innerHTML = JSON.stringify(j, null, 2)
    }
    
    create_job_req.send(job_body);
}

function cleanup() {
  stopLiveFeed()
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
    let resume_job_url = api_server_url + "jobs/";
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
