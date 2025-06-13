// main.go
package main

import (
	"log"
	"time"

	"github.com/run2go/time-automation/config"
	"github.com/run2go/time-automation/executor"
	"github.com/run2go/time-automation/notify"
	"github.com/run2go/time-automation/scheduler"
	"github.com/run2go/time-automation/tracker"
)

func main() {
	cfg := config.Load()

	// Do NOT clear state file at startup; keep for resuming and metrics
	log.Println("[INIT] Configuration loaded.")

	notifier := notify.New(cfg.WebhookURL)
	exec := executor.New(*cfg)
	state := tracker.New(cfg.StateFile)
	sched := scheduler.New(*cfg, state, exec, notifier)

	log.Println("[START] Time automation running...")
	for {
		sched.Run()
		time.Sleep(time.Minute)
	}
}
