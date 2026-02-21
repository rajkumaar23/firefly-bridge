package state

import (
	"encoding/json"
	"os"
	"time"
)

// SkipWindow is the minimum time that must elapse since an institution was last
// successfully processed before it will be processed again.
const SkipWindow = time.Hour

// ProcessingState tracks the last successful processing time for each institution.
type ProcessingState struct {
	Institutions map[string]time.Time `json:"institutions"`
}

// Load reads ProcessingState from the JSON file at path. If the file does not
// exist, an empty state is returned so the first run proceeds without error.
func Load(path string) (*ProcessingState, error) {
	s := &ProcessingState{Institutions: make(map[string]time.Time)}
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
