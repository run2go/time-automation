// scheduler/scheduler.go
package scheduler

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/run2go/time-automation/config"
	"github.com/run2go/time-automation/executor"
	"github.com/run2go/time-automation/notify"
	"github.com/run2go/time-automation/tracker"
)

type Scheduler struct {
	cfg      config.Config
	state    *tracker.StateTracker
	executor *executor.Executor
	notify   *notify.Notifier

	randomizedTimes map[string]time.Time
	randomizedDay   int

	holidayCachePath  string
	vacationCachePath string
	lastCalendarFetch int
}

func New(cfg config.Config, s *tracker.StateTracker, e *executor.Executor, n *notify.Notifier) *Scheduler {
	return &Scheduler{
		cfg:             cfg,
		state:           s,
		executor:        e,
		notify:          n,
		randomizedTimes: make(map[string]time.Time),
		randomizedDay:   -1,
	}
}

// Randomize a time between min and max (HH:MM), including random seconds
func randomizeTimeRange(base time.Time, minStr, maxStr string) time.Time {
	minT, _ := time.Parse("15:04", minStr)
	maxT, _ := time.Parse("15:04", maxStr)
	min := time.Date(base.Year(), base.Month(), base.Day(), minT.Hour(), minT.Minute(), 0, 0, base.Location())
	max := time.Date(base.Year(), base.Month(), base.Day(), maxT.Hour(), maxT.Minute(), 59, 0, base.Location())
	span := max.Sub(min)
	if span <= 0 {
		return min
	}
	sec := rand.Int63n(int64(span.Seconds()) + 1)
	return min.Add(time.Duration(sec) * time.Second)
}

func randomDuration(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int63n(int64(max-min+1)))
}

// Call this once per day to randomize all times and print them
func (s *Scheduler) randomizeAllTimes(now time.Time) {
	today := now.Format("2006-01-02")
	st := s.state.Load(today)

	// Use planned times from state if present, otherwise randomize and persist
	var startWork, startBreak, stopBreak, stopWork time.Time

	if !st.PlannedStartWork.IsZero() {
		startWork = st.PlannedStartWork
	} else if st.WorkStarted && !st.WorkStartTime.IsZero() {
		startWork = st.WorkStartTime
		st.PlannedStartWork = startWork
	} else {
		startWork = randomizeTimeRange(now, s.cfg.StartWorkMin.Format("15:04"), s.cfg.StartWorkMax.Format("15:04"))
		st.PlannedStartWork = startWork
	}

	if !st.PlannedStartBreak.IsZero() {
		startBreak = st.PlannedStartBreak
	} else if st.BreakStarted && !st.BreakStartTime.IsZero() {
		startBreak = st.BreakStartTime
		st.PlannedStartBreak = startBreak
	} else {
		startBreak = randomizeTimeRange(now, s.cfg.StartBreakMin.Format("15:04"), s.cfg.StartBreakMax.Format("15:04"))
		st.PlannedStartBreak = startBreak
	}

	if !st.PlannedStopBreak.IsZero() {
		stopBreak = st.PlannedStopBreak
	} else {
		minBreak := s.cfg.MinBreakDuration
		maxBreak := s.cfg.MaxBreakDuration
		breakDuration := randomDuration(minBreak, maxBreak)
		stopBreak = startBreak.Add(breakDuration)
		st.PlannedStopBreak = stopBreak
	}

	if !st.PlannedStopWork.IsZero() {
		stopWork = st.PlannedStopWork
	} else {
		minWork := s.cfg.MinWorkDuration
		maxWork := s.cfg.MaxWorkDuration
		workDuration := randomDuration(minWork, maxWork)
		minBreak := s.cfg.MinBreakDuration
		maxBreak := s.cfg.MaxBreakDuration
		breakDuration := stopBreak.Sub(startBreak)
		// If break duration is not plausible, randomize
		if breakDuration <= 0 {
			breakDuration = randomDuration(minBreak, maxBreak)
		}
		stopWork = startWork.Add(workDuration + breakDuration)
		st.PlannedStopWork = stopWork
	}

	// Save planned times into state only if they were missing
	s.state.Save(today, st)

	s.randomizedTimes = map[string]time.Time{
		"START_WORK":  startWork,
		"START_BREAK": startBreak,
		"STOP_BREAK":  stopBreak,
		"STOP_WORK":   stopWork,
	}
	s.randomizedDay = now.YearDay()

	// Prepare and send planned times notification (log only once)
	plannedMsg := fmt.Sprintf(
		"[PLANNED TIMES]\n  START_WORK: %s\n  START_BREAK: %s\n  STOP_BREAK: %s\n  STOP_WORK: %s",
		startWork.Format("15:04:05"),
		startBreak.Format("15:04:05"),
		stopBreak.Format("15:04:05"),
		stopWork.Format("15:04:05"),
	)
	log.Println(plannedMsg)
	// Only send notification if webhook is set
	if s.notify != nil && s.cfg.WebhookURL != "" {
		s.notify.Send("Planned Times", plannedMsg)
	}
}

func (s *Scheduler) fetchCalendar(url, cachePath string, useAuth bool) ([]byte, error) {
	if url == "" {
		return nil, nil
	}
	var resp *http.Response
	var err error
	var data []byte

	if useAuth && url != "" {
		// Use HTTP Basic Auth for vacation calendar
		client := &http.Client{}
		req, errReq := http.NewRequest("GET", url, nil)
		if errReq != nil {
			log.Printf("[INIT] Failed to create request for %s: %v", url, errReq)
			goto fallback
		}
		username := s.cfg.Username + "@" + s.cfg.Domain
		password := s.cfg.Password
		req.SetBasicAuth(username, password)
		resp, err = client.Do(req)
		if err != nil {
			log.Printf("[INIT] Failed to fetch vacation calendar: %v", err)
		}
	} else if url != "" {
		resp, err = http.Get(url)
		if err != nil {
			log.Printf("[INIT] Failed to fetch holiday calendar: %v", err)
		}
	} else {
		goto fallback
	}

	if err != nil || resp.StatusCode != 200 {
		log.Printf("[INIT] Failed to fetch calendar from %s, using cache if available", url)
		goto fallback
	}
	defer resp.Body.Close()
	data, err = ioutil.ReadAll(resp.Body)
	if err == nil {
		_ = ioutil.WriteFile(cachePath, data, 0644)
		log.Printf("[INIT] Successfully fetched and cached calendar: %s", url)
	}
	return data, err

fallback:
	// fallback to cached file
	data, err2 := ioutil.ReadFile(cachePath)
	if err2 == nil {
		log.Printf("[INIT] Loaded cached calendar for %s", url)
		return data, nil
	}
	log.Printf("[INIT] No calendar available for %s", url)
	return nil, err
}

func (s *Scheduler) isTodayHolidayOrVacation(now time.Time) (bool, string) {
	day := now.YearDay()
	// Only fetch if URLs are set
	if s.lastCalendarFetch != day {
		s.lastCalendarFetch = day
		log.Println("[INIT] Fetching holiday and vacation calendars...")
		if s.cfg.HolidayAddress != "" {
			_, _ = s.fetchCalendar(s.cfg.HolidayAddress, "holiday.ics", false)
		}
		if s.cfg.VacationAddress != "" {
			_, _ = s.fetchCalendar(s.cfg.VacationAddress, "vacation.ics", true)
		}
	}
	if s.cfg.HolidayAddress != "" && isICSToday("holiday.ics", now, "") {
		log.Printf("[SCHEDULER] Today is a public holiday (%s)", now.Format("2006-01-02"))
		return true, "Public holiday"
	}
	if s.cfg.VacationAddress != "" && isICSToday("vacation.ics", now, s.cfg.VacationKeyword) {
		log.Printf("[SCHEDULER] Today is a vacation day (%s)", now.Format("2006-01-02"))
		return true, "Vacation"
	}
	return false, ""
}

// Checks if today is in the ICS file, optionally filtering by keyword
func isICSToday(path string, now time.Time, keyword string) bool {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	dateStr := now.Format("20060102")
	inEvent := false
	hasKeyword := keyword == ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "BEGIN:VEVENT" {
			inEvent = true
			hasKeyword = keyword == ""
		}
		if inEvent && strings.HasPrefix(line, "SUMMARY:") && keyword != "" {
			if strings.Contains(line, keyword) {
				hasKeyword = true
			}
		}
		if inEvent && strings.HasPrefix(line, "DTSTART") && strings.Contains(line, dateStr) && hasKeyword {
			return true
		}
		if line == "END:VEVENT" {
			inEvent = false
		}
	}
	return false
}

func (s *Scheduler) Run() {
	now := time.Now()

	// Holiday/vacation check
	if (s.cfg.HolidayAddress != "" || s.cfg.VacationAddress != "") && (s.cfg.HolidayAddress != "" || s.cfg.VacationAddress != "") {
		if isHoliday, reason := s.isTodayHolidayOrVacation(now); isHoliday {
			msg := ""
			if reason == "Public holiday" {
				msg = fmt.Sprintf("[HOLIDAY]\n  Today is holiday: %s", now.Format("2006-01-02"))
			} else if reason == "Vacation" {
				msg = fmt.Sprintf("[VACATION]\n  Today is vacation: %s", now.Format("2006-01-02"))
			} else {
				msg = fmt.Sprintf("No work today: %s (%s)", reason, now.Format("2006-01-02"))
			}
			log.Println("[SCHEDULER]", msg)
			if s.notify != nil && s.cfg.WebhookURL != "" {
				s.notify.Send(reason, msg)
			}
			// --- Add entry to state.json for this day ---
			today := now.Format("2006-01-02")
			st := s.state.Load(today)
			st.DayNote = reason
			s.state.Save(today, st)
			return
		}
	}

	if !isWorkDay(s.cfg.WorkDays, now) {
		return // skip if not a work day
	}
	// Randomize times once per day (at midnight or first run of the day)
	if s.randomizedDay != now.YearDay() {
		s.randomizeAllTimes(now)
	}

	today := now.Format("2006-01-02")
	st := s.state.Load(today)

	if now.After(s.randomizedTimes["START_WORK"]) && !st.WorkStarted {
		log.Println("[SCHEDULER] Triggering StartWork")
		s.executor.StartWork()
		st.WorkStarted = true
		st.WorkStartTime = time.Now()
		s.state.Save(today, st)
		return
	}

	// Only start break if it has NOT been started AND NOT been stopped (i.e., not completed)
	if st.WorkStarted && !st.BreakStarted && !st.BreakStopped && now.After(s.randomizedTimes["START_BREAK"]) {
		log.Println("[SCHEDULER] Triggering StartBreak")
		s.executor.StartBreak()
		st.BreakStarted = true
		st.BreakStartTime = time.Now()
		s.state.Save(today, st)
		return
	}

	if st.BreakStarted && !st.BreakStopped && now.After(s.randomizedTimes["STOP_BREAK"]) {
		if time.Since(st.BreakStartTime) >= s.cfg.MinBreakDuration {
			s.executor.StopBreak()
			st.BreakStopped = true
			s.state.Save(today, st)
		} else {
			log.Println("[INFO] Not stopping break: minimum duration not met.")
		}
		return
	}

	if st.WorkStarted && !st.WorkStopped && now.After(s.randomizedTimes["STOP_WORK"]) {
		if time.Since(st.WorkStartTime) >= s.cfg.MinWorkDuration {
			s.executor.StopWork()
			st.WorkStopped = true
			s.state.Save(today, st)
			// Reset state for today after work is finished, so next day starts fresh
			s.state.Reset(today)
			s.state.LastResetDay = now.YearDay()
		} else {
			log.Println("[INFO] Not stopping work: minimum duration not met.")
		}
		return
	}
}

func isWorkDay(workDays string, now time.Time) bool {
	// workDays: e.g. "1-5", "1,3,5", "0,6"
	if workDays == "" {
		return true // default: run every day
	}
	weekday := int(now.Weekday()) // Sunday=0, Monday=1, ..., Saturday=6
	for _, part := range strings.Split(workDays, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) == 2 {
				start, _ := strconv.Atoi(bounds[0])
				end, _ := strconv.Atoi(bounds[1])
				if start <= weekday && weekday <= end {
					return true
				}
			}
		} else {
			val, _ := strconv.Atoi(part)
			if val == weekday {
				return true
			}
		}
	}
	return false
}
