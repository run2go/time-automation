// config/config.go
package config

import (
	"log"
	"os"
	"time"
)

type Config struct {
	Domain     string
	Subdomain  string
	Username   string
	Password   string
	WebhookURL string
	StateFile  string

	StartWorkMin  time.Time
	StartWorkMax  time.Time
	StartBreakMin time.Time
	StartBreakMax time.Time
	StopBreakMin  time.Time
	StopBreakMax  time.Time
	StopWorkMin   time.Time
	StopWorkMax   time.Time

	MinWorkDuration  time.Duration
	MaxWorkDuration  time.Duration
	MinBreakDuration time.Duration
	MaxBreakDuration time.Duration

	WorkDays string // e.g. "1-5"

	Task    string
	Verbose bool
	DryRun  bool

	// New fields for holiday/vacation
	HolidayAddress  string
	VacationAddress string
	VacationKeyword string
}

func Load() *Config {
	parseTime := func(key string) time.Time {
		val := os.Getenv(key)
		t, err := time.Parse("15:04", val)
		if err != nil {
			log.Fatalf("Invalid time format for %s: %s", key, val)
		}
		return t
	}

	parseDuration := func(key string) time.Duration {
		val := os.Getenv(key)
		d, err := time.ParseDuration(val)
		if err != nil {
			log.Fatalf("Invalid duration for %s: %s", key, val)
		}
		return d
	}

	cfg := &Config{
		Domain:     os.Getenv("DOMAIN"),
		Subdomain:  os.Getenv("SUBDOMAIN"),
		Username:   os.Getenv("USERNAME"),
		Password:   os.Getenv("PASSWORD"),
		WebhookURL: os.Getenv("WEBHOOK_URL"),
		StateFile:  os.Getenv("STATE_FILE"),

		StartWorkMin:  parseTime("START_WORK_MIN"),
		StartWorkMax:  parseTime("START_WORK_MAX"),
		StartBreakMin: parseTime("START_BREAK_MIN"),
		StartBreakMax: parseTime("START_BREAK_MAX"),

		MinWorkDuration:  parseDuration("MIN_WORK_DURATION"),
		MaxWorkDuration:  parseDuration("MAX_WORK_DURATION"),
		MinBreakDuration: parseDuration("MIN_BREAK_DURATION"),
		MaxBreakDuration: parseDuration("MAX_BREAK_DURATION"),

		WorkDays: os.Getenv("WORK_DAYS"),

		Task:    os.Getenv("TASK"),
		Verbose: os.Getenv("VERBOSE") == "true",
		DryRun:  os.Getenv("DRY_RUN") == "true",

		// New fields
		HolidayAddress:  os.Getenv("HOLIDAY_ADDRESS"),
		VacationAddress: os.Getenv("VACATION_ADDRESS"),
		VacationKeyword: os.Getenv("VACATION_KEYWORD"),
	}

	return cfg
}
