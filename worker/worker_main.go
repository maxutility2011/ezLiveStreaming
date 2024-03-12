package worker

import (
	"fmt"
	"net/http"
	"strings"
	"encoding/json"
    "os/exec"
    "live_transcoder_server/api_server_main"
	//"github.com/google/uuid"
	//"io/ioutil"
)

// live_worker -f job.json 
// live_worker -p [job_json] 
func main() {
    var jobSpecPath
    if len(os.Args) == 2 {
        jobSpecPath = os.Args[1]
    }

    job_spec_string, err := os.Open(jobSpecPath)
    if err != nil {
        fmt.Println(err)
    }

    var j api_server_main.liveJobSpec

    defer job_spec.Close() 

    // ffmpeg -f flv -listen 1 -i [live_input] -vf scale=w=640:h=360 -c:v libx264 -profile:v baseline -an -use_template 1 -adaptation_sets "id=0,streams=v id=1,streams=a" -seg_duration 4 -utc_timing_url https://time.akamai.com/?iso -window_size 15 -extra_window_size 15 -remove_at_exit 1 -f dash /var/www/html/[job_ib]/1.mpd

    cmd := exec.Command("git", "checkout", "develop")
}