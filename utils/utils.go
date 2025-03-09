package utils

import (
	"errors"
	"strconv"
	"io/ioutil"
	"os"
	"path"
	"strings"
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

func Read_file(path string) ([]byte, error) {
	var data []byte
	f, err := os.Open(path)
	if err != nil {
		return data, err
	}

	defer f.Close()
	data, _ = ioutil.ReadAll(f)
	return data, nil
}

func Write_file(data []byte, file_name string) error {
	err := os.WriteFile(file_name, data, 0644)
    return err
}

func Get_path_dir(p string) string {
	return path.Dir(p)
}

func Get_path_filename(p string) string {
	_, filename := path.Split(p)
	return filename
}

// new_extension: any random string representing the new file extension, 
// e.g., ".merged", ".detected"
func Change_file_extension(p string, new_extension string) string {
	pos_dot := strings.LastIndex(p, ".") 
	return (p[:pos_dot] + new_extension)
}