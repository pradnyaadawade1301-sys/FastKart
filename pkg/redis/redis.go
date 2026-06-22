package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

var client *redis.Client
var available bool

func Init(addr string) {
	client = redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		available = false
	} else {
		available = true
	}
}

func IsAvailable() bool {
	return available
}

func Get(key string) (string, bool) {
	if !available {
		return "", false
	}
	val, err := client.Get(context.Background(), key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func Set(key string, value interface{}, duration time.Duration) {
	if !available {
		return
	}
	client.Set(context.Background(), key, value, duration)
}
func Connect(addr string) {
	Init(addr)
}
