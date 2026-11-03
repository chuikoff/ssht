package tunnel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RunningTunnelRecord represents a persisted active tunnel process.
type RunningTunnelRecord struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// RunningTracker manages cross-process persistence of active tunnel PIDs.
type RunningTracker struct {
	mu       sync.Mutex
	filePath string
}

func NewRunningTracker() *RunningTracker {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return &RunningTracker{
		filePath: filepath.Join(home, ".ssh", "ssht_running.json"),
	}
}

// NewRunningTrackerWithPath creates a tracker with a specific file path.
func NewRunningTrackerWithPath(path string) *RunningTracker {
	return &RunningTracker{
		filePath: path,
	}
}

func (r *RunningTracker) Load() map[string]RunningTunnelRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	records := r.loadLocked()
	changed := false
	for id, rec := range records {
		if !isProcessAlive(rec.PID) {
			delete(records, id)
			changed = true
		}
	}
	if changed {
		_ = r.saveLocked(records)
	}

	return records
}

func (r *RunningTracker) SetRunning(id string, pid int, startedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	records := r.loadLocked()
	records[id] = RunningTunnelRecord{
		PID:       pid,
		StartedAt: startedAt,
	}
	_ = r.saveLocked(records)
}

func (r *RunningTracker) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	records := r.loadLocked()
	if _, ok := records[id]; ok {
		delete(records, id)
		_ = r.saveLocked(records)
	}
}

func (r *RunningTracker) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	_ = os.Remove(r.filePath)
}

func (r *RunningTracker) loadLocked() map[string]RunningTunnelRecord {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return make(map[string]RunningTunnelRecord)
	}
	var records map[string]RunningTunnelRecord
	if err := json.Unmarshal(data, &records); err != nil || records == nil {
		return make(map[string]RunningTunnelRecord)
	}
	return records
}

func (r *RunningTracker) saveLocked(records map[string]RunningTunnelRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	tmpFile := r.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}
	_ = os.Remove(r.filePath)
	return os.Rename(tmpFile, r.filePath)
}
