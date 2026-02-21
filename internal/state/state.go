package state

import (
	"encoding/json"
	"os"
	"time"
)

// SkipWindow is the minimum time that must elapse since an institution was last
// successfully processed before it will be processed again.
const SkipWindow = time.Hour

// ProcessingState tracks the last successful processing time for each
// institution and, at a finer grain, for each account within an institution.
type ProcessingState struct {
	Institutions map[string]time.Time            `json:"institutions"`
	Accounts     map[string]map[string]time.Time `json:"accounts"`
}

// Load reads ProcessingState from the JSON file at path. If the file does not
// exist, an empty state is returned so the first run proceeds without error.
func Load(path string) (*ProcessingState, error) {
	s := &ProcessingState{
		Institutions: make(map[string]time.Time),
		Accounts:     make(map[string]map[string]time.Time),
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	// Guard against state files written before the Accounts field existed.
	if s.Accounts == nil {
		s.Accounts = make(map[string]map[string]time.Time)
	}
	return s, nil
}

// Save writes the current state to the JSON file at path, creating or
// overwriting it. It should be called after each institution is successfully
// processed so that progress is persisted even if a later institution fails.
func (s *ProcessingState) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LastAccountSync returns the time the given account was last successfully
// synced, or the zero value if it has never been synced.
func (s *ProcessingState) LastAccountSync(institution, account string) time.Time {
	if m, ok := s.Accounts[institution]; ok {
		return m[account]
	}
	return time.Time{}
}

// RecordAccountSync records the current time as the last successful sync for
// the given account.
func (s *ProcessingState) RecordAccountSync(institution, account string) {
	if s.Accounts[institution] == nil {
		s.Accounts[institution] = make(map[string]time.Time)
	}
	s.Accounts[institution][account] = time.Now()
}
