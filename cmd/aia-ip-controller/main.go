package main

import (
	"math/rand"
	"os"
	"time"

	"k8s.io/component-base/logs"
	"tkestack.io/aia-ip-controller/cmd/aia-ip-controller/app"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	cmd := app.NewControllerCommand()
	logs.InitLogs()
	defer logs.FlushLogs()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
