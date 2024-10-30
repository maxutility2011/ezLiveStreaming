package utils

import (
	"errors"
	"strconv"
)

var valid_bitrate_units = []string{"k", "K"}

func BitrateString2Float64(bs string) (error, float64) {
	var r float64 = -1
	if !(bs[len(bs)-1] == 'k' || bs[len(bs)-1] == 'K') {
		return errors.New("invalid_bitrate_string"), r
	}

	b, err := strconv.ParseFloat(bs[:len(bs)-1], 64)
	if err != nil {
		return err, r
	}

	r = b * 1000
	return nil, r
}