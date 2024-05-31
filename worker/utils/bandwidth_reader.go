package main

import (
	"fmt"
	"time"
	"strconv"
	"os/exec"
)

func main() {
	for {
		iftopCmd := exec.Command("sh", "start_iftop.sh", "1935")
		out, err_iftop := iftopCmd.CombinedOutput() 
		if err_iftop != nil {
			fmt.Printf("Errors starting iftop. Error: %v, iftop output: %s", err_iftop, string(out))
        }

		iftop_output := string(out)
		bandwidth_unit := iftop_output[len(iftop_output) - 3 : len(iftop_output) - 1]
		bandwidth_value := iftop_output[: len(iftop_output) - 3]

		bandwidth, err := strconv.ParseFloat(bandwidth_value, 64)
		if err != nil {
			fmt.Println("Invalid bandwidth reading: ", bandwidth_value)
			return
		}

		if bandwidth_unit == "Mb" {
			bandwidth *= 1000
		}

		fmt.Printf("bandwidth: %f, unit: %s", bandwidth, bandwidth_unit)
		time.Sleep(time.Second)
	}
}