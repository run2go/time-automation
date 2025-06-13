// tracker/state.go
package tracker

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"
)

type DayState struct {
	WorkStarted    bool      `json:"work_started"`
	WorkStartTime  time.Time `json:"work_start_time"`
	WorkStopped    bool      `json:"work_stopped"`
	BreakStarted   bool      `json:"break_started"`
	BreakStartTime time.Time `json:"break_start_time"`
	BreakStopped   bool      `json:"break_stopped"`
	// Planned times for this day
	PlannedStartWork  time.Time `json:"planned_start_work,omitempty"`
	PlannedStartBreak time.Time `json:"planned_start_break,omitempty"`
	PlannedStopBreak  time.Time `json:"planned_stop_break,omitempty"`
	PlannedStopWork   time.Time `json:"planned_stop_work,omitempty"`
	DayNote           string    `json:"day_note,omitempty"` // <-- Add this line
}

// Returns the total duration (work + break) for this day.
// If work or break is not stopped, uses current time as end.
func (ds *DayState) TotalDuration() time.Duration {
	now := time.Now()
	var workEnd, breakEnd time.Time

	if ds.WorkStarted {
		if ds.WorkStopped {
			workEnd = ds.WorkStartTime.Add(ds.workDuration())
		} else {
			workEnd = now
		}
	} else {
		return 0
	}

	workDuration := workEnd.Sub(ds.WorkStartTime)

	breakDuration := time.Duration(0)
	if ds.BreakStarted {
		if ds.BreakStopped {
			breakEnd = ds.BreakStartTime.Add(ds.breakDuration())
		} else {
			breakEnd = now
		}
		breakDuration = breakEnd.Sub(ds.BreakStartTime)
	}

	return workDuration + breakDuration
}

// Helper: duration between WorkStartTime and now or when stopped
func (ds *DayState) workDuration() time.Duration {
	if ds.WorkStarted {
		if ds.WorkStopped {
			return ds.WorkStartTime.Sub(ds.WorkStartTime)
		}
		return time.Since(ds.WorkStartTime)
	}
	return 0
}

// Helper: duration between BreakStartTime and now or when stopped
func (ds *DayState) breakDuration() time.Duration {
	if ds.BreakStarted {
		if ds.BreakStopped {
			return ds.BreakStartTime.Sub(ds.BreakStartTime)
		}
		return time.Since(ds.BreakStartTime)
	}
	return 0
}

type StateTracker struct {
	path         string
	lock         sync.Mutex
	data         map[string]DayState
	LastResetDay int
}

type State struct {
	WorkStarted    bool
	BreakStarted   bool
	BreakStopped   bool
	WorkStopped    bool
	WorkStartTime  time.Time
	BreakStartTime time.Time
	LastResetDay   int
}

func New(path string) *StateTracker {
	return &StateTracker{
		path: path,
		data: make(map[string]DayState),
	}
}

func (s *StateTracker) Reset(date string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[date] = DayState{}
	s.saveFile()
}

func (s *StateTracker) Load(date string) DayState {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.loadFile()

	if state, ok := s.data[date]; ok {
		return state
	}
	return DayState{}
}

func (s *StateTracker) Save(date string, state DayState) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[date] = state
	s.saveFile()
}

func (s *StateTracker) loadFile() {
	file, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer file.Close()
	content, _ := ioutil.ReadAll(file)
	json.Unmarshal(content, &s.data)
}

func (s *StateTracker) saveFile() {
	file, err := os.Create(s.path)
	if err != nil {
		log.Printf("[STATE] Failed to open state file for writing: %v", err)
		return
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.data); err != nil {
		log.Printf("[STATE] Failed to encode state: %v", err)
	}
	if err := file.Sync(); err != nil {
		log.Printf("[STATE] Failed to sync state file: %v", err)
	}
}

func (s *State) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("state.json", data, 0644)
}
