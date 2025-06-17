// executor/timeapi.go
package executor

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/run2go/time-automation/config"
	"github.com/run2go/time-automation/notify"
)

type Executor struct {
	cfg      config.Config
	token    string
	notifier *notify.Notifier
}

func New(cfg config.Config) *Executor {
	return &Executor{
		cfg:      cfg,
		notifier: notify.New(cfg.WebhookURL),
	}
}

func (e *Executor) VerboseLog(msg string) {
	if e.cfg.Verbose {
		log.Println("[VERBOSE]", msg)
	}
}

func (e *Executor) login() string {
	payload := map[string]string{
		"username": e.cfg.Username,
		"password": e.cfg.Password,
	}
	data, _ := json.Marshal(payload)
	url := "https://" + e.cfg.Subdomain + "." + e.cfg.Domain + "/api/login"
	log.Println("[LOGIN] Attempting login at:", url)
	e.VerboseLog("POST " + url)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		msg := "Login failed: " + err.Error()
		log.Println("[LOGIN] " + msg)
		e.notifier.Send("Login Failed", msg)
		return ""
	}
	defer resp.Body.Close()

	var rawResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&rawResp)
	rawBytes, _ := json.Marshal(rawResp)
	log.Println("[LOGIN] Raw response:", string(rawBytes))
	e.VerboseLog("Login response: " + string(rawBytes))

	token, _ := rawResp["token"].(string)
	if token == "" {
		msg := "Login failed: no token received"
		log.Println("[LOGIN] " + msg)
		e.notifier.Send("Login Failed", msg)
	} else {
		log.Println("[LOGIN] Token received successfully")
	}
	return token
}

func (e *Executor) post(status interface{}) {
	log.Printf("[POST] Preparing to post time entry with status: %v", status)
	if e.cfg.DryRun {
		log.Println("[POST] DRY_RUN enabled: would POST /api/post-time")
		e.VerboseLog("DRY_RUN enabled: would POST /api/post-time with status: " + toString(status))
		e.VerboseLog("Payload: " + toString(map[string]interface{}{
			"status":     status,
			"inputValue": e.cfg.Task,
			"userid":     e.cfg.Username,
		}))
		return
	}
	if e.token == "" {
		log.Println("[POST] No token cached, logging in...")
		e.token = e.login()
		if e.token == "" {
			log.Println("[POST] No token, aborting post")
			e.VerboseLog("No token, aborting post")
			return
		}
	}

	var payload map[string]interface{}
	task := e.cfg.Task

	// Bash logic: for break stop (status == true), override task
	if status == true {
		task = "Pauseneintrag - Status: Pause auto"
	}

	// Bash logic: status is string for work, bool for break
	switch v := status.(type) {
	case string:
		// "Start" or "Stop" for work
		payload = map[string]interface{}{
			"status":     v,
			"inputValue": task,
			"userid":     e.cfg.Username,
		}
	case bool:
		// true/false for break
		payload = map[string]interface{}{
			"status":     v,
			"inputValue": task,
			"userid":     e.cfg.Username,
		}
	default:
		// fallback
		payload = map[string]interface{}{
			"status":     status,
			"inputValue": task,
			"userid":     e.cfg.Username,
		}
	}

	data, _ := json.Marshal(payload)
	url := "https://" + e.cfg.Subdomain + "." + e.cfg.Domain + "/api/post-time"
	log.Println("[POST] POST", url, "payload:", string(data))
	e.VerboseLog("POST " + url + " payload: " + string(data))

	var resp *http.Response
	var err error
	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(data))
		req.Header.Set("Authorization", e.token)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		log.Printf("[POST] Attempt %d failed: %v", attempt, err)
		time.Sleep(1 * time.Second)
	}
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := "Failed to post after 5 attempts: " + err.Error()
		log.Println("[POST] " + msg)
		e.notifier.Send("Post Failed @here", msg)
		return
	}
	defer resp.Body.Close()
	log.Printf("[POST] Posted status: %v", status)
	e.VerboseLog("Posted status: " + toString(status))
}

func toString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (e *Executor) StartWork()  { e.post("Start") }
func (e *Executor) StopWork()   { e.post("Stop") }
func (e *Executor) StartBreak() { e.post(false) }
func (e *Executor) StopBreak()  { e.post(true) }
