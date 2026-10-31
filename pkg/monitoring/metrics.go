package monitoring

import (
	"time"
)

// ServerMetrics contains parsed system telemetry from a remote host.
type ServerMetrics struct {
	Host        string
	OS          string
	Uptime      string
	Load1       string
	Load5       string
	Load15      string
	CPUPercent  float64 // 0 - 100%
	RAMUsedMB   uint64
	RAMTotalMB  uint64
	RAMPercent  float64
	SwapUsedMB  uint64
	SwapTotalMB uint64
	DiskUsedMB  uint64
	DiskTotalMB uint64
	DiskPercent float64
	DiskMount   string
	FetchedAt   time.Time
	RawOutput   string
	Error       string
}
