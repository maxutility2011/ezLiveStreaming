package main

import (
	"ezliveStreaming/s3"
)

func main() {
	s3.Upload("/tmp/1.mp4", "bozhang-private")
}