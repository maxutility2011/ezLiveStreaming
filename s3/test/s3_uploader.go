package main

import (
	"ezliveStreaming/s3"
	"fmt"
)

func main() {
	err := s3.Upload("/tmp/1.mp4", "1.mp4", "bzhang-test-bucket-public/output/")
	if err != nil {
		fmt.Println("Error: ", err)
	}
}