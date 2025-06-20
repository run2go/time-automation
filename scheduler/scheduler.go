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

	holidayCheckedDay  int
	holidayCheckedType string

	calendarFetchedDay int // Track last day calendars were fetched
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
		"START_WORK:  %s\nSTART_BREAK: %s\nSTOP_BREAK:  %s\nSTOP_WORK:   %s",
		startWork.Format("15:04:05"),
		startBreak.Format("15:04:05"),
		stopBreak.Format("15:04:05"),
		stopWork.Format("15:04:05"),
	)
	log.Println("[PLANNED TIMES]\n" + plannedMsg)
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

func (s *Scheduler) isTodayHolidayOrVacation(now time.Time) (bool, string, string) {
	// Fetch calendars only once per day
	if s.calendarFetchedDay != now.YearDay() {
		s.calendarFetchedDay = now.YearDay()
		log.Println("[INIT] Fetching holiday and vacation calendars...")
		if s.cfg.HolidayAddress != "" {
			_, _ = s.fetchCalendar(s.cfg.HolidayAddress, "holiday.ics", false)
		}
		if s.cfg.VacationAddress != "" {
			_, _ = s.fetchCalendar(s.cfg.VacationAddress, "vacation.ics", true)
		}
	}
	// Try to extract holiday name if present
	holidayName := ""
	if s.cfg.HolidayAddress != "" {
		holidayName = getICSTodaySummary("holiday.ics", now)
		if holidayName != "" {
			log.Printf("[SCHEDULER] Today is a public holiday (%s): %s", now.Format("2006-01-02"), holidayName)
			return true, "Public holiday", holidayName
		}
	}
	if s.cfg.VacationAddress != "" && isICSToday("vacation.ics", now, s.cfg.VacationKeyword) {
		log.Printf("[SCHEDULER] Today is a vacation day (%s)", now.Format("2006-01-02"))
		return true, "Vacation", ""
	}
	return false, "", ""
}

// Helper: get the SUMMARY for today's event in an ICS file
func getICSTodaySummary(path string, now time.Time) string {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	dateStr := now.Format("20060102")
	inEvent := false
	summary := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "BEGIN:VEVENT" {
			inEvent = true
			summary = ""
		}
		if inEvent && strings.HasPrefix(line, "SUMMARY:") {
			summary = strings.TrimPrefix(line, "SUMMARY:")
		}
		if inEvent && strings.HasPrefix(line, "DTSTART") && strings.Contains(line, dateStr) {
			return summary
		}
		if line == "END:VEVENT" {
			inEvent = false
		}
	}
	return ""
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

func (s *Scheduler) verboseLog(msg string) {
	if s.executor != nil {
		s.executor.VerboseLog(msg)
	}
}

func (s *Scheduler) Run() {
	now := time.Now()

	// Only check for holiday/vacation if today is a workday
	if isWorkDay(s.cfg.WorkDays, now) {
		today := now.Format("2006-01-02")
		st := s.state.Load(today)

		// Only check and notify if not already marked as holiday/vacation in state and not already checked in memory
		if !(st.IsHoliday || st.IsVacation) && s.holidayCheckedDay != now.YearDay() {
			if isHoliday, reason, holidayName := s.isTodayHolidayOrVacation(now); isHoliday {
				var msg string
				checkedType := ""
				if reason == "Public holiday" {
					if holidayName != "" {
						msg = fmt.Sprintf("%s\n%s", holidayName, now.Format("2006-01-02"))
					} else {
						msg = fmt.Sprintf("Public holiday\n%s", now.Format("2006-01-02"))
					}
					st.IsHoliday = true
					checkedType = "holiday"
				} else if reason == "Vacation" {
					msg = fmt.Sprintf("Vacation\n%s", now.Format("2006-01-02"))
					st.IsVacation = true
					checkedType = "vacation"
				} else {
					msg = fmt.Sprintf("No work today: %s (%s)", reason, now.Format("2006-01-02"))
					checkedType = "other"
				}
				log.Println("[SCHEDULER]", msg)
				if s.notify != nil && s.cfg.WebhookURL != "" {
					s.notify.Send(reason, msg)
				}
				st.DayNote = reason
				s.state.Save(today, st)
				s.holidayCheckedDay = now.YearDay()
				s.holidayCheckedType = checkedType
				return
			}
		} else if st.IsHoliday || st.IsVacation || s.holidayCheckedDay == now.YearDay() {
			// Already marked or checked, skip further actions for today
			return
		}
	} else {
		// Not a workday, do nothing (no notification, no state update)
		return
	}

	// Randomize times once per day (at midnight or first run of the day)
	if s.randomizedDay != now.YearDay() {
		s.randomizeAllTimes(now)
	}

	today := now.Format("2006-01-02")
	st := s.state.Load(today)

	// --- Prevent any further actions if the full cycle is done for the day ---
	if st.WorkStarted && st.WorkStopped && st.BreakStarted && st.BreakStopped {
		// All actions for the day are done, do nothing more
		return
	}

	if now.After(s.randomizedTimes["START_WORK"]) && !st.WorkStarted {
		log.Println("[SCHEDULER] Triggering StartWork")
		s.executor.StartWork()
		st.WorkStarted = true
		st.WorkStartTime = time.Now()
		s.state.Save(today, st)
		log.Printf("[STATE] Updated: work_started=true for %s", today)
		return
	}

	if st.WorkStarted && !st.BreakStarted && !st.BreakStopped && now.After(s.randomizedTimes["START_BREAK"]) {
		log.Println("[SCHEDULER] Triggering StartBreak")
		s.executor.StartBreak()
		st.BreakStarted = true
		st.BreakStartTime = time.Now()
		s.state.Save(today, st)
		log.Printf("[STATE] Updated: break_started=true for %s", today)
		return
	}

	if st.BreakStarted && !st.BreakStopped {
		plannedStopBreak := s.randomizedTimes["STOP_BREAK"]
		minBreakMet := time.Since(st.BreakStartTime) >= s.cfg.MinBreakDuration
		afterPlannedStop := now.After(plannedStopBreak)
		if minBreakMet && afterPlannedStop {
			s.executor.StopBreak()
			st.BreakStopped = true
			s.state.Save(today, st)
			log.Printf("[STATE] Updated: break_stopped=true for %s", today)
		} else if !minBreakMet {
			s.verboseLog("[INFO] Not stopping break: minimum duration not met.")
		} else if !afterPlannedStop {
			s.verboseLog("[INFO] Not stopping break: planned stop time not reached.")
		}
		return
	}

	if st.WorkStarted && !st.WorkStopped && now.After(s.randomizedTimes["STOP_WORK"]) {
		if time.Since(st.WorkStartTime) >= s.cfg.MinWorkDuration {
			s.executor.StopWork()
			st.WorkStopped = true
			s.state.Save(today, st)
			log.Printf("[STATE] Updated: work_stopped=true for %s", today)
			// Do not reset state here; keep the day's state for metrics and to prevent re-triggering
		} else {
			s.verboseLog("[INFO] Not stopping work: minimum duration not met.")
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
