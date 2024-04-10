var manifestUri_aurora_stats = "";
var streamCreationTime = 0;

var loadButton11 = document.getElementById('load-url-11');
loadButton11.addEventListener('click', (event) => {
    reloadAuroraStream();
});

var createButton11 = document.getElementById('create-url-11');
createButton11.addEventListener('click', (event) => {
    createStream();
});

var video11 = document.getElementById('video11');
    
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

    aurora_stats_hls.attachMedia(video11);
    aurora_stats_hls.loadSource(manifestUri_aurora_stats);
}

function reloadAuroraStream() {
    let currentTime = Date.now();
    if (currentTime - streamCreationTime > 15000) {
        console.log(manifestUri_aurora_stats);
        aurora_stats_hls.loadSource(manifestUri_aurora_stats);
    } else {
        window.alert("Stream not ready to play. Please wait up to 15 seconds");
    }
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
