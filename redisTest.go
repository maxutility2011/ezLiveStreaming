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

func main() {
	/*client := redis.NewClient(&redis.Options{
		Addr: "172.17.0.4:6379",
		Password: "",
		DB: 0,
	})

	ctx := context.Background()
	pong, err := client.Ping(ctx).Result()
	fmt.Println(pong, err)*/

	redisClient.RedisIp ="172.17.0.4"
	redisClient.RedisPort = "6379"
	redisClient.Client, redisClient.Ctx = redisClient.CreateClient(redisClient.RedisIp, redisClient.RedisPort)

	j := Job{
		Id: "7da9bb1c-c862-4e54-897e-500b3356eb16", 
		Input: "rtmp://localhost:1935/live/app",
	}

	b, e1 := json.Marshal(j)
	if e1 != nil {
		fmt.Println("Failed to marshal JSON")
	}

	e2 := redisClient.SetKVString(j.Id, string(b), 0)
	if e2 != nil {
		fmt.Println("Failed to set key/value in Redis")
		return
	}

	v, e3 := redisClient.GetKV(j.Id)
	if e3 != nil {
		fmt.Println("Failed to get key/value from Redis")
		return
	}

	fmt.Println("Value from Redis: ", v)
	
/*
	val, e := client.Get(ctx, j.Id).Result()
	if e != nil {
		fmt.Println("Error: ", e)
		return 
	}
	
	fmt.Println("val: ", val)
	*/
}