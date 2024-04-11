var playback_url = "http://localhost:4080/ezliveStreaming/1.mp4";
var create_button;
var stop_button;
var resume_button;
var job_request;
var video;

window.addEventListener("DOMContentLoaded", (event) => {
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
    
    job_request = document.getElementById('job_request');
    
    video = document.getElementById('video');
});
    
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

function createJob() {
    let create_job_url = "http://localhost:1080/jobs";
    let create_job_req = new XMLHttpRequest();
    create_job_req.open("POST", create_job_url, true);
    create_job_req.setRequestHeader("Content-Type", "application/json");

    create_job_req.onload = function (e) {
        if (create_job_req.readyState === create_job_req.DONE) {
          if (create_job_req.status === 201) {
            let job_resp = this.response;
            window.alert(job_resp);
          } else {
            console.log("create new live job failed: " + create_job_req.status);
          }
        }
    }

    let job_body = ""
    if (job_request.value != "") {
        job_body = job_request.value;
    }
    
    //let data = JSON.stringify(job_body);
    //window.alert(job_body);
    create_job_req.send(job_body);
    //console.log(manifestUri_aurora_stats);
    //aurora_stats_hls.loadSource(manifestUri_aurora_stats);

    //window.alert("Stream not ready to play. Please wait up to 15 seconds");
}

function createStream() {
    let create_url = "http://ec2-13-59-210-247.us-east-2.compute.amazonaws.com:1080/createBasicLiveStream";
    let create_req = new XMLHttpRequest();
    create_req.open("POST", create_url, true);

    create_req.onload = function (e) {
        if (create_req.readyState === create_req.DONE) {
          if (create_req.status === 201) {
            let stream_playback_id = this.response;
            console.log("stream_playback_id: " + stream_playback_id);
            manifestUri_aurora_stats = stream_playback_id;
            streamCreationTime = Date.now();
          }
          else {
            console.log("create new stream failed: " + create_req.status);
          }
        }
    }

    create_req.send();
}
