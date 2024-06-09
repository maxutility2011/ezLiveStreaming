package main

import (
	//"strconv"
	"fmt"
	"regexp"
	"strings"
	//"os"
	"flag"
	"os/exec"
)

func main() {
	pidPtr := flag.String("pid", "", "process ID to monitor")
	flag.Parse()

	cpuReaderCmd := exec.Command("sh", "read_cpuutil.sh", *pidPtr)
	out, err := cpuReaderCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Errors starting cpuReader. Error: %v, iftop output: %s", err, string(out))
	} else {
		ps_output := string(out)
		fmt.Printf("ps output: %s\n", ps_output)
		re := regexp.MustCompile(".[0-9]") // CPU util is a float, so match all the digits and "."
		fmt.Println("FindAllString", re.FindAllString(ps_output, -1))
		r := strings.Join(re.FindAllString(ps_output, -1), "")
		fmt.Printf("CPU util: %s\n", r)
	}
}