package sshconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HostStatus represents online availability status.
type HostStatus string

const (
	StatusUnknown  HostStatus = "UNKNOWN"
	StatusChecking HostStatus = "CHECKING"
	StatusOnline   HostStatus = "ONLINE"
	StatusOffline  HostStatus = "OFFLINE"
	StatusProxy    HostStatus = "PROXY"
)

// HostMeta holds persistent and runtime metadata for an SSH host.
type HostMeta struct {
	LastConnected  *time.Time `json:"last_connected,omitempty"`
	Archived       bool       `json:"archived,omitempty"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	ArchivedReason string     `json:"archived_reason,omitempty"`
	Status         HostStatus `json:"-"`
	LatencyMs      int64      `json:"-"`
	LastError      string     `json:"-"`
}

// MetadataStore manages persistence of host metadata in ~/.ssh/ssht_metadata.json.
type MetadataStore struct {
	mu       sync.RWMutex
	filePath string
	Data     map[string]*HostMeta // key: primary host alias or hostname
}

func NewMetadataStore() (*MetadataStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(sshDir, "ssht_metadata.json")

	store := &MetadataStore{
		filePath: path,
		Data:     make(map[string]*HostMeta),
	}
	_ = store.Load()
	return store, nil
}

// NewMetadataStoreWithPath creates a store with a custom file path.
func NewMetadataStoreWithPath(path string) *MetadataStore {
	store := &MetadataStore{
		filePath: path,
		Data:     make(map[string]*HostMeta),
	}
	_ = store.Load()
	return store
}

func (s *MetadataStore) DataLock() {
	s.mu.Lock()
}

func (s *MetadataStore) DataUnlock() {
	s.mu.Unlock()
}

func (s *MetadataStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return nil
	}

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(bytes))) == 0 {
		return nil
	}

	var data map[string]*HostMeta
	if err := json.Unmarshal(bytes, &data); err != nil {
		corruptPath := fmt.Sprintf("%s.corrupt.%d", s.filePath, time.Now().Unix())
		_ = os.WriteFile(corruptPath, bytes, 0600)
		return fmt.Errorf("metadata file corrupted (backup saved to %s): %w", corruptPath, err)
	}
	s.Data = data
	return nil
}

func (s *MetadataStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(s.filePath, bytes, 0600)
}

func (s *MetadataStore) Get(key string) *HostMeta {
	s.mu.Lock()
	defer s.mu.Unlock()

	if meta, exists := s.Data[key]; exists {
		return meta
	}
	meta := &HostMeta{Status: StatusUnknown}
	s.Data[key] = meta
	return meta
}

func (s *MetadataStore) RecordConnection(key string) {
	now := time.Now()
	s.mu.Lock()
	meta, exists := s.Data[key]
	if !exists {
		meta = &HostMeta{}
		s.Data[key] = meta
	}
	meta.LastConnected = &now
	s.mu.Unlock()

	_ = s.Save()
}

func (s *MetadataStore) SetStatus(key string, status HostStatus, latencyMs int64, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, exists := s.Data[key]
	if !exists {
		meta = &HostMeta{}
		s.Data[key] = meta
	}
	meta.Status = status
	meta.LatencyMs = latencyMs
	meta.LastError = errMsg
}

func (s *MetadataStore) SetAllStatus(status HostStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, meta := range s.Data {
		meta.Status = status
	}
}

// IsArchived returns true if the host is in the quarantine / archive.
func (s *MetadataStore) IsArchived(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if meta, exists := s.Data[key]; exists {
		return meta.Archived
	}
	return false
}

// SetArchived marks or unmarks a host as quarantined.
func (s *MetadataStore) SetArchived(key string, archived bool, reason string) {
	now := time.Now()
	s.mu.Lock()
	meta, exists := s.Data[key]
	if !exists {
		meta = &HostMeta{}
		s.Data[key] = meta
	}
	meta.Archived = archived
	if archived {
		meta.ArchivedAt = &now
		meta.ArchivedReason = reason
	} else {
		meta.ArchivedAt = nil
		meta.ArchivedReason = ""
	}
	s.mu.Unlock()

	_ = s.Save()
}

// ToggleArchived switches between archived and active state.
func (s *MetadataStore) ToggleArchived(key string) bool {
	s.mu.Lock()
	meta, exists := s.Data[key]
	if !exists {
		meta = &HostMeta{}
		s.Data[key] = meta
	}
	meta.Archived = !meta.Archived
	now := time.Now()
	if meta.Archived {
		meta.ArchivedAt = &now
		if meta.ArchivedReason == "" {
			meta.ArchivedReason = "Перемещен пользователем"
		}
	} else {
		meta.ArchivedAt = nil
		meta.ArchivedReason = ""
	}
	newStatus := meta.Archived
	s.mu.Unlock()

	_ = s.Save()
	return newStatus
}

// ArchivedCount returns the count of hosts in the quarantine.
func (s *MetadataStore) ArchivedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, meta := range s.Data {
		if meta.Archived {
			count++
		}
	}
	return count
}

// FormatLastConnected produces a readable string for the last connection time.
func FormatLastConnected(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "Никогда"
	}

	now := time.Now()
	diff := now.Sub(*t)

	if diff < 1*time.Minute {
		return "Только что"
	}
	if diff < 60*time.Minute {
		return fmt.Sprintf("%d мин. назад", int(diff.Minutes()))
	}

	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()

	if y1 == y2 && m1 == m2 && d1 == d2 {
		return fmt.Sprintf("Сегодня, %02d:%02d", t.Hour(), t.Minute())
	}

	yesterday := now.AddDate(0, 0, -1)
	yy, ym, yd := yesterday.Date()
	if yy == y2 && ym == m2 && yd == d2 {
		return fmt.Sprintf("Вчера, %02d:%02d", t.Hour(), t.Minute())
	}

	if y1 == y2 {
		return fmt.Sprintf("%02d.%02d %02d:%02d", t.Day(), t.Month(), t.Hour(), t.Minute())
	}

	return t.Format("2006-01-02 15:04")
}

// PingHost tests TCP connectivity to the SSH host.
func PingHost(h *HostConfig, timeout time.Duration) (bool, int64, error) {
	if h.HasProxyJump() {
		return false, 0, errors.New("PROXY_JUMP")
	}

	target := h.HostName
	if target == "" && len(h.Aliases) > 0 {
		target = h.Aliases[0]
	}
	port := h.Port
	if port <= 0 {
		port = 22
	}

	addr := net.JoinHostPort(target, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false, 0, err
	}
	_ = conn.Close()
	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}
