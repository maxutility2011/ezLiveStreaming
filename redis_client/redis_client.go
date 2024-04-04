package redis_client

import (
	"fmt"
	"time"
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9" // redis_client.go is ONLY tested against go-redis v9!!! 
)

type RedisClient struct {
	RedisIp string
	RedisPort string
	Ctx context.Context
	Client *redis.Client
}

type RedisConfig struct {
	RedisIp string
	RedisPort string
}

// The following constants define all the Redis keys including hash tables (accessed by HSET/HGET) 
// and variables (accessed by SET/GET)
const REDIS_KEY_ALLJOBS = "jobs"
const REDIS_KEY_WAITINGSET = "waiting_set"
const REDIS_KEY_PENDINGSET = "pending_set"
const REDIS_KEY_DONESET = "done_set"
const REDIS_KEY_ALLWORKERS = "workers"
const REDIS_KEY_NUMWORKERS = "num_workers"
const REDIS_KEY_NUMJOBS = "num_jobs"

func (rc RedisClient) CreateClient(redis_ip string, redis_port string) (*redis.Client, context.Context) {
	redisAddr := redis_ip + ":" + redis_port
	fmt.Println("Creating Redis client and connecting to redisAddr: ", redisAddr)
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		Password: "",
		DB: 0,
	})

	return client, context.Background()
}

// HSET (value is string)
// htable: the hash table that the k/v is to be inserted. For example, "job1" (k) with its value (v)
//	       is inserted to a table called "jobs".
// k: HSET field 
// v: HSET value
func (rc RedisClient) HSetStruct(htable string, k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("Failed to marshal JSON in redis client")
		return err
	}	

	err = rc.Client.HSet(rc.Ctx, htable, k, string(b)).Err()
	return err
}

// HSET (value is struct)
func (rc RedisClient) HSetString(htable string, k string, v string) error {
	err := rc.Client.HSet(rc.Ctx, htable, k, v).Err()
	return err
}

// HGET
// htable: the hash table that the k/v is to be got from. 
// k: HGET member 
func (rc RedisClient) HGet(htable string, k string) (string, error) {
	v, err := rc.Client.HGet(rc.Ctx, htable, k).Result()
	return v, err
}

func (rc RedisClient) HGetAll(htable string) ([]string, error) {
	var allKeys []string
	var allVals []string
	allKeys, e := rc.Client.HKeys(rc.Ctx, htable).Result()
	if e != nil {
		return allVals, e
	}

	var err error
	var v string
	for _, k := range allKeys {
		v, err = rc.Client.HGet(rc.Ctx, htable, k).Result()
		if err != nil {
			allVals = nil
			break
		}

		allVals = append(allVals, v)
	}

	return allVals, err
}

// HSCAN
// htable: the hash table that the k/v is to be got from. 
// k: HSCAN key 
func (rc RedisClient) HScan(htable string) ([]string, error) {
	// In go-redis v9, HSCAN.Result() returns "keys, cursor, err"
	keys, _, err := rc.Client.HScan(rc.Ctx, htable, 0, "", 0).Result()
	return keys, err
}

func (rc RedisClient) HKeys(htable string) ([]string, error) {
	keys, err := rc.Client.HKeys(rc.Ctx, htable).Result()
	return keys, err
}

// SET (value type is struct)
func (rc RedisClient) SetKVStruct(k string, v any, timeout time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("Failed to marshal JSON in redis client")
		return err
	}

	err = rc.Client.Set(rc.Ctx, k, string(b), timeout).Err()
	return err
}

// SET (value type is string)
func (rc RedisClient) SetKVString(k string, v string, timeout time.Duration) error {
	err := rc.Client.Set(rc.Ctx, k, v, timeout).Err()
	return err
}

// GET
func (rc RedisClient) GetKV(k string) (string, error) {
	v, err := rc.Client.Get(rc.Ctx, k).Result()
	return v, err
}

func (rc RedisClient) HDelOne(htable string, k string) error {
	_, err := rc.Client.HDel(rc.Ctx, htable, k).Result()
	return err
}

// Dangerous!!! Test ONLY!!!
func (rc RedisClient) HDelAll(htable string) error {
	keys, err := rc.Client.HKeys(rc.Ctx, htable).Result()
	if err != nil {
		return err
	}

	for _, k := range keys {
		_, err = rc.Client.HDel(rc.Ctx, htable, k).Result()
	}

	return err
}