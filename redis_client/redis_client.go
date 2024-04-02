package redis_client

import (
	"fmt"
	"time"
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	RedisIp string
	RedisPort string
	Ctx context.Context
	Client *redis.Client
}

func (rc RedisClient) CreateClient(redis_ip string, redis_port string) (*redis.Client, context.Context) {
	//rc.RedisIp = redis_ip
	//rc.RedisPort = redis_port
	redisAddr := redis_ip + ":" + redis_port
	fmt.Println("redisAddr: ", redisAddr)
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		Password: "",
		DB: 0,
	})

	//rc.Client = client
	return client, context.Background()
}

func (rc RedisClient) SetKVStruct(k string, v any, timeout time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("Failed to marshal JSON")
		return err
	}

	err = rc.Client.Set(rc.Ctx, k, string(b), timeout).Err()
	if (err != nil) {
		fmt.Println("Failed to set key/value in redis")
		return err
	}

	return nil
}

func (rc RedisClient) SetKVString(k string, v string, timeout time.Duration) error {
	err := rc.Client.Set(rc.Ctx, k, v, timeout).Err()
	if (err != nil) {
		fmt.Println("Failed to set key/value in redis")
		return err
	}

	return nil
}

func (rc RedisClient) GetKV(k string) (string, error) {
	v, err := rc.Client.Get(rc.Ctx, k).Result()
	if err != nil {
		fmt.Println("Error: ", err)
		return "", err
	}

	return v, nil
}

/*
func main() {
	client := redis.NewClient(&redis.Options{
		Addr: "172.17.0.4:6379",
		Password: "",
		DB: 0,
	})

	ctx := context.Background()
	pong, err := client.Ping(ctx).Result()
	fmt.Println(pong, err)

	j := Job{
		Id: "7da9bb1c-c862-4e54-897e-500b3356eb16", 
		Input: "rtmp://localhost:1935/live/app",
	}

	b, e2 := json.Marshal(j)
	if e2 != nil {
		fmt.Println("Failed to marshal JSON")
	}

	err = client.Set(ctx, j.Id, string(b), 0).Err()

	val, e := client.Get(ctx, j.Id).Result()
	if e != nil {
		fmt.Println("Error: ", e)
		return 
	}
	
	fmt.Println("val: ", val)
}
*/