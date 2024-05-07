package models

import (
	"crypto/rand"
	"time"
	"fmt"
)

var DrmKeyFileName = "decryption.key"

type CreateKeyRequest struct {
	Content_id string
}

type CreateKeyResponse struct {
    Key_id string
    Content_id string
    Time_created time.Time
}

type KeyInfo struct {
	Key_id string
	Key string
    Content_id string
    Time_created time.Time
}

func Random_16bytes_as_string() (string, error) {
	rand_16bytes := ""

	c := 16
	b := make([]byte, c)
	_, err := rand.Read(b)
	if err != nil {
		return rand_16bytes, err
	}

	for _, i := range b {
		h := fmt.Sprintf("%x", i)
		rand_16bytes += h
	}

	return rand_16bytes, nil
}