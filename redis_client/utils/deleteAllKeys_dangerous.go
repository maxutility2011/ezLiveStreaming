package main

import (
	"fmt"
	"ezliveStreaming/redis_client"
	"flag"
)

var redisClient redis_client.RedisClient

func main() {
	ipAddrPtr := flag.String("ipaddr", "", "Redis IP address")
	portPtr := flag.String("port", "", "Redis port")
	tablePtr := flag.String("table", "", "The hash table to be deleted")
	flag.Parse()

	if *ipAddrPtr == "" {
		redisClient.RedisIp = "127.0.0.1"
	} else {
		redisClient.RedisIp = *ipAddrPtr
	}

	if *portPtr == "" {
		redisClient.RedisPort = "6379"
	} else {
		redisClient.RedisPort = *portPtr
	}

	var htable string
	if *tablePtr == "" {
		fmt.Println("Missing hash table name (to be deleted)")
		return
	} else {
		htable = *tablePtr
	}

	redisClient.Client, redisClient.Ctx = redisClient.CreateClient(redisClient.RedisIp, redisClient.RedisPort)
	
	// First, show all the keys 
	vals, e := redisClient.HKeys(htable)
	if e != nil {
		fmt.Println("Failed to HKEYS in Redis. Error: ", e)
		return
	}

	for _, element := range vals {
		fmt.Println("key: ", element)
	}
	
	numFound := len(vals)

	// Now, delete all
	err := redisClient.HDelAll(htable)
	if err != nil {
		fmt.Println("Failed to delete keys in Redis. Error: ", err)
		return
	}

	// Then, verify all the keys are deleted
	vals1, e := redisClient.HKeys(htable)
	if e != nil {
		fmt.Println("Failed to HKEYS in Redis. Error: ", e)
		return
	}

	for _, element := range vals1 {
		fmt.Println("key: ", element)
	}

	fmt.Println("Deleted ", numFound, " keys")
}

