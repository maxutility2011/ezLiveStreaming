package models

import (
	"time"
)

type LiveWorker struct {
	Id string
	HttpBaseUrl string
	RtmpBaseUrl string
	Registered_at time.Time
}