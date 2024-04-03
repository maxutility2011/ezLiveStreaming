package main

import (
	"fmt"
	"encoding/json"
	"ezliveStreaming/redis_client"
)

var redisClient redis_client.RedisClient

type Job struct {
	Id string
	Input string
}

func testSET(k string, v string) {
	e1 := redisClient.SetKVString(k, v, 0)
	if e1 != nil {
		fmt.Println("Failed to set key/value in Redis")
		return
	}

	r, e2 := redisClient.GetKV(k)
	if e2 != nil {
		fmt.Println("Failed to get key/value from Redis")
		return
	}

	fmt.Println("Value from Redis: ", r)
}

func testHSETString(htable string, k string, v string) {
	e1 := redisClient.HSetString(htable, k, v)
	if e1 != nil {
		fmt.Println("Failed to set key/value in Redis")
		return
	}

	r, e2 := redisClient.HGet(htable, k)
	if e2 != nil {
		fmt.Println("Failed to get key/value from Redis")
		return
	}

	fmt.Println("Value from Redis: ", r)
}

func testHSETStruct(htable string, k string, v any) {
	e1 := redisClient.HSetStruct(htable, k, v)
	if e1 != nil {
		fmt.Println("Failed to set key/value in Redis")
		return
	}

	r, e2 := redisClient.HGet(htable, k)
	if e2 != nil {
		fmt.Println("Failed to get key/value from Redis")
		return
	}

	fmt.Println("Value from Redis: ", r)
}

func testHScan(htable string) {
	vals, e1 := redisClient.HScan(htable)
	if e1 != nil {
		fmt.Println("Failed to set key/value in Redis")
		return
	}

	for i, element := range vals {
		fmt.Println("key: ", i, " value: ", element)
	}
}

func testHKeys(htable string) {
	vals, e1 := redisClient.HKeys(htable)
	if e1 != nil {
		fmt.Println("Failed to set key/value in Redis")
		return
	}

	for i, element := range vals {
		fmt.Println("key: ", i, " value: ", element)
	}
}

func main() {
	redisClient.RedisIp ="172.17.0.4"
	redisClient.RedisPort = "6379"
	redisClient.Client, redisClient.Ctx = redisClient.CreateClient(redisClient.RedisIp, redisClient.RedisPort)

	j1 := Job{
		Id: "7da9bb1c-c862-4e54-897e-500b3356eb16", 
		Input: "rtmp://localhost:1935/live/app",
	}

	b, e1 := json.Marshal(j1)
	if e1 != nil {
		fmt.Println("Failed to marshal JSON")
	}

	testSET(j1.Id, string(b))

	jobs := "jobs"
	testHSETStruct(jobs, j1.Id, j1)

	j2 := Job{
		Id: "8da9bb1c-c862-4e54-897e-500b3356eb16", 
		Input: "rtmp://localhost:1935/live/app2",
	}

	testHSETStruct(jobs, j2.Id, j2)
	testHKeys(jobs)
	//testHScan(jobs)

	//testHSETString(jobs, j.Id, string(b))
}