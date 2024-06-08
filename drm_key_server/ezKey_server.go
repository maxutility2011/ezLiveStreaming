package main

import (
	"encoding/json"
	"ezliveStreaming/models"
	"ezliveStreaming/redis_client"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type KeyServerConfig struct {
	Server_hostname string
	Server_port     string
	Redis           redis_client.RedisConfig
}

var Log *log.Logger
var server_hostname string
var server_port string
var key_endpoint = "keys"
var keys []models.KeyInfo
var redis redis_client.RedisClient
var key_server_config_file_path string
var key_server_config KeyServerConfig

func createUpdateDrmKey(k models.KeyInfo) error {
	err := redis.HSetStruct(redis_client.REDIS_KEY_DRM_KEYS, k.Key_id, k)
	if err != nil {
		Log.Println("Failed to create/update DRM key id=", k.Key_id, ". Error: ", err)
	}

	return err
}

func getKeyById(kid string) (models.KeyInfo, bool) {
	var k models.KeyInfo
	v, err := redis.HGet(redis_client.REDIS_KEY_DRM_KEYS, kid)
	if err != nil {
		Log.Println("Failed to find key id=", kid, ". Error: ", err)
		return k, false
	}

	err = json.Unmarshal([]byte(v), &k)
	if err != nil {
		Log.Println("Failed to unmarshal Redis result (getKeyById). Error: ", err)
		return k, false
	}

	return k, true
}

func main_server_handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=ascii")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,access-control-allow-origin, access-control-allow-headers")
	w.Header().Set("Access-Control-Allow-Methods", "*")

	if (*r).Method == "OPTIONS" {
		return
	}

	Log.Println("----------------------------------------")
	Log.Println("Received new request:")
	Log.Println(r.Method, r.URL.Path)

	posLastSingleSlash := strings.LastIndex(r.URL.Path, "/")
	UrlLastPart := r.URL.Path[posLastSingleSlash+1:]

	// Remove trailing "/" if any
	if len(UrlLastPart) == 0 {
		path_without_trailing_slash := r.URL.Path[0:posLastSingleSlash]
		posLastSingleSlash = strings.LastIndex(path_without_trailing_slash, "/")
		UrlLastPart = path_without_trailing_slash[posLastSingleSlash+1:]
	}

	if strings.Contains(r.URL.Path, key_endpoint) {
		if !(r.Method == "GET" || r.Method == "POST") {
			err := "Method = " + r.Method + " is not allowed to " + r.URL.Path
			Log.Println(err)
			http.Error(w, "405 method not allowed\n  Error: "+err, http.StatusMethodNotAllowed)
			return
		}

		if r.Method == "POST" {
			if r.Body == nil {
				res := "Error New DRM key request without body"
				Log.Println(res)
				http.Error(w, "400 bad request\n  Error: "+res, http.StatusBadRequest)
				return
			}

			var kr models.CreateKeyRequest
			err := json.NewDecoder(r.Body).Decode(&kr)
			if err != nil {
				res := "Failed to decode create key request"
				Log.Println(res)
				http.Error(w, "400 bad request\n  Error: "+res, http.StatusBadRequest)
				return
			}

			var k models.KeyInfo
			kid, err1 := models.Random_16bytes_as_string()
			key, err2 := models.Random_16bytes_as_string()
			k.Time_created = time.Now()
			k.Content_id = kr.Content_id

			if err1 == nil && err2 == nil {
				k.Key_id = kid
				k.Key = key
				createUpdateDrmKey(k)

				FileContentType := "application/json"
				w.Header().Set("Content-Type", FileContentType)
				w.WriteHeader(http.StatusCreated)

				var resp models.CreateKeyResponse
				resp.Key_id = k.Key_id
				resp.Content_id = k.Content_id
				resp.Time_created = k.Time_created

				json.NewEncoder(w).Encode(resp)
			} else {
				res := "Error: Failed to create new DRM key."
				Log.Println(res)
				http.Error(w, "500 internal server error\n Error: "+res, http.StatusInternalServerError)
			}
		} else if r.Method == "GET" { // Get one key: /keys/[key_id]
			if UrlLastPart == key_endpoint {
				res := "Error: A valid key ID must be provided"
				Log.Println(res)
				http.Error(w, "400 bad request\n Error: "+res, http.StatusBadRequest)
				return
			}

			kid := UrlLastPart
			k, ok := getKeyById(kid)
			if ok {
				FileContentType := "application/json"
				w.Header().Set("Content-Type", FileContentType)
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(k)
				return
			} else {
				res := "Error: Non-existent key id: " + kid
				Log.Println(res)
				http.Error(w, res, http.StatusNotFound)
				return
			}
		}
	}
}

func readConfig() {
	configFile, err := os.Open(key_server_config_file_path)
	if err != nil {
		fmt.Println(err)
	}

	defer configFile.Close()
	config_bytes, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(config_bytes, &key_server_config)
}

func main() {
	configPtr := flag.String("config", "", "config file path")
	flag.Parse()

	if *configPtr != "" {
		key_server_config_file_path = *configPtr
	} else {
		key_server_config_file_path = "config.json"
	}

	readConfig()
	redis.RedisIp = key_server_config.Redis.RedisIp
	redis.RedisPort = key_server_config.Redis.RedisPort
	redis.Client, redis.Ctx = redis.CreateClient(redis.RedisIp, redis.RedisPort)

	var logfile, err1 = os.Create("/home/streamer/log/key_server.log")
	if err1 != nil {
		panic(err1)
	}

	Log = log.New(logfile, "", log.LstdFlags)

	server_addr := key_server_config.Server_hostname + ":" + key_server_config.Server_port
	fmt.Println("DRM key server listening on: ", server_addr)
	http.HandleFunc("/", main_server_handler)
	http.ListenAndServe(server_addr, nil)
}
