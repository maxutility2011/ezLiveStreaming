package main

import (
	"fmt"
	//"encoding/json"
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
		fmt.Println("Failed to SET in Redis. Error: ", e1)
		return
	}

	r, e2 := redisClient.GetKV(k)
	if e2 != nil {
		fmt.Println("Failed to GET from Redis. Error: ", e2)
		return
	}

	fmt.Println("Value from Redis: ", r)
}

func testHSETString(htable string, k string, v string) {
	e1 := redisClient.HSetString(htable, k, v)
	if e1 != nil {
		fmt.Println("Failed to HSET in Redis. Error: ", e1)
		return
	}

	r, e2 := redisClient.HGet(htable, k)
	if e2 != nil {
		fmt.Println("Failed to HGET from Redis. Error: ", e2)
		return
	}

	fmt.Println("Value from Redis: ", r)
}

func testHSETStruct(htable string, k string, v any) {
	e1 := redisClient.HSetStruct(htable, k, v)
	if e1 != nil {
		fmt.Println("Failed to HSET in Redis. Error: ", e1)
		return
	}

	r, e2 := redisClient.HGet(htable, k)
	if e2 != nil {
		fmt.Println("Failed to HGET from Redis. Error: ", e2)
		return
	}

	fmt.Println("Value from Redis: ", r)
}

func testHScan(htable string) {
	vals, e1 := redisClient.HScan(htable)
	if e1 != nil {
		fmt.Println("Failed to HSCAN in Redis. Error: ", e1)
		return
	}

	for i, element := range vals {
		fmt.Println("key: ", i, " value: ", element)
	}
}

func testHKeys(htable string) {
	vals, e1 := redisClient.HKeys(htable)
	if e1 != nil {
		fmt.Println("Failed to HKEYS in Redis. Error: ", e1)
		return
	}

	for i, element := range vals {
		fmt.Println("key: ", i, " value: ", element)
	}
}

func testHGet(htable string, k string) {
	v, e := redisClient.HGet(htable, k)
	if e != nil {
		fmt.Println("Failed to HGET in Redis. Error: ", e)
	}

	fmt.Println("Value: ", v)
}

func testHGetAll(htable string) {
	vals, e := redisClient.HGetAll(htable)
	if e != nil {
		fmt.Println("Failed to HGETALL in Redis. Error: ", e)
		return
	}

	for _, v := range vals {
		fmt.Println("Value: ", v)
	}
}

func testQueue() {
	fmt.Println("testQueue starts............................")
	var err error
	err = redisClient.QPush("testQueue", "e1")
	err = redisClient.QPush("testQueue", "e2")
	err = redisClient.QPush("testQueue", "e3")
	err = redisClient.QPush("testQueue", "e4")
	if err != nil {
		fmt.Println("Failed to QPush in Redis. Error: ", err)
		return
	} 

	length, err0 := redisClient.QLen("testQueue")
	if err0 == nil {
		fmt.Println("Queue length = ", length)
	} else {
		fmt.Println("Failed to QLen in Redis. Error: ", err0)
		return
	}

	front1, err1 := redisClient.QFront("testQueue")
	if err1 == nil {
		fmt.Println("Queue front: ", front1)
	} else {
		fmt.Println("Failed to QFront in Redis. Error: ", err1)
		return
	}

	front2, err2 := redisClient.QPop("testQueue")
	if err2 == nil {
		fmt.Println("Queue front: ", front2, " popped out")
	} else {
		fmt.Println("Failed to QPop in Redis. Error: ", err2)
		return
	}

	front3, err3 := redisClient.QFront("testQueue")
	if err3 == nil {
		fmt.Println("Queue front: ", front3)
	} else {
		fmt.Println("Failed to QFront in Redis. Error: ", err3)
		return
	}

	fmt.Println("testQueue ends............................")
}

func main() {
	redisClient.RedisIp ="172.17.0.5"
	redisClient.RedisPort = "6379"
	redisClient.Client, redisClient.Ctx = redisClient.CreateClient(redisClient.RedisIp, redisClient.RedisPort)

	jobs := "testjobs"
	j1 := Job{
		Id: "7da9bb1c-c862-4e54-897e-500b3356eb16", 
		Input: "rtmp://localhost:1935/live/app",
	}

	testHSETStruct(jobs, j1.Id, j1)

	j2 := Job{
		Id: "8da9bb1c-c862-4e54-897e-500b3356eb16", 
		Input: "rtmp://localhost:1935/live/app2",
	}

	testHSETStruct(jobs, j2.Id, j2)

	j2.Input = "rtmp://localhost:1935/live/app22"
	testHSETStruct(jobs, j2.Id, j2)
	
	//testHKeys(jobs)
	//testHGetAll(jobs)

	redisClient.HDelAll(jobs)

	testQueue()
	//testHGet(jobs, j2.Id)
	//testHScan(jobs)

	//testHSETString(jobs, j.Id, string(b))
}