var playback_url = "http://localhost:4080/ezliveStreaming/1.mp4";
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

    show_button = document.getElementById('show');
    show_button.addEventListener('click', (event) => {
        showJob();
    });

    response_code = document.getElementById('response_code');

    response_body = document.getElementById('response_body');
    
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
            job_id = j.Id
            response_code.innerHTML = "status code=" + show_job_req.status
            response_body.innerHTML = JSON.stringify(j, null, 2)
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
            response_body.innerHTML = JSON.stringify(JSON.parse(job_resp), null, 2)
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
            response_body.innerHTML = JSON.stringify(JSON.parse(job_resp), null, 2)
            //window.alert(job_resp);
          } else {
            console.log("stop new live job failed. Status code:" + resume_job_req.status);
          }
        }
    }
    
    resume_job_req.send();
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
