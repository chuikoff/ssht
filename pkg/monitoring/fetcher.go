package monitoring

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const probeScript = `
echo "===UNAME==="
uname -s -r -m 2>/dev/null || uname
echo "===UPTIME==="
uptime 2>/dev/null || true
echo "===MEM==="
free -m 2>/dev/null || true
echo "===DISK==="
df -m / 2>/dev/null || df / 2>/dev/null
echo "===CPU==="
top -b -n 1 2>/dev/null | head -n 8 || true
`

// FetchMetrics runs a non-interactive remote diagnostics script via OpenSSH.
func FetchMetrics(host string, timeout time.Duration) (*ServerMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new",
		host,
		probeScript,
	)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return &ServerMetrics{
			Host:      host,
			FetchedAt: time.Now(),
			Error:     errMsg,
		}, fmt.Errorf("failed to connect: %s", errMsg)
	}

	metrics := ParseOutput(stdoutBuf.String())
	metrics.Host = host
	metrics.FetchedAt = time.Now()
	metrics.RawOutput = stdoutBuf.String()
	return metrics, nil
}

// ParseOutput extracts structured system metrics from the probe script output.
func ParseOutput(raw string) *ServerMetrics {
	m := &ServerMetrics{
		OS:        "Linux / Unix",
		Uptime:    "Неизвестно",
		Load1:     "-",
		Load5:     "-",
		Load15:    "-",
		DiskMount: "/",
	}

	sections := strings.Split(raw, "===")
	for i := 1; i < len(sections); i += 2 {
		tag := strings.TrimSpace(sections[i])
		var body string
		if i+1 < len(sections) {
			body = sections[i+1]
		}

		switch tag {
		case "UNAME":
			lines := strings.Split(strings.TrimSpace(body), "\n")
			if len(lines) > 0 && lines[0] != "" {
				m.OS = strings.TrimSpace(lines[0])
			}

		case "UPTIME":
			parseUptime(body, m)

		case "MEM":
			parseMemory(body, m)

		case "DISK":
			parseDisk(body, m)

		case "CPU":
			parseCPU(body, m)
		}
	}

	return m
}

func parseUptime(body string, m *ServerMetrics) {
	line := strings.TrimSpace(body)
	if line == "" {
		return
	}

	// Extract uptime: e.g. "12:34:56 up 14 days,  3:22,  2 users,  load average: 0.15, 0.22, 0.18"
	if upIdx := strings.Index(line, "up "); upIdx != -1 {
		rest := line[upIdx+3:]
		if userIdx := strings.Index(rest, "user"); userIdx != -1 {
			beforeUsers := strings.TrimSpace(rest[:userIdx])
			if commaIdx := strings.LastIndex(beforeUsers, ","); commaIdx != -1 {
				m.Uptime = strings.TrimSpace(beforeUsers[:commaIdx])
			} else {
				m.Uptime = strings.TrimSpace(beforeUsers)
			}
		}
	}

	// Extract load averages
	if loadIdx := strings.Index(line, "load average:"); loadIdx != -1 {
		loads := strings.TrimSpace(line[loadIdx+13:])
		parts := strings.Split(loads, ",")
		if len(parts) >= 1 {
			m.Load1 = strings.TrimSpace(parts[0])
		}
		if len(parts) >= 2 {
			m.Load5 = strings.TrimSpace(parts[1])
		}
		if len(parts) >= 3 {
			m.Load15 = strings.TrimSpace(parts[2])
		}
	}
}

func parseMemory(body string, m *ServerMetrics) {
	lines := strings.Split(body, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "Mem:") {
			total, err1 := strconv.ParseUint(fields[1], 10, 64)
			used, err2 := strconv.ParseUint(fields[2], 10, 64)
			if err1 == nil && err2 == nil && total > 0 {
				m.RAMTotalMB = total
				m.RAMUsedMB = used
				m.RAMPercent = float64(used) / float64(total) * 100
				if m.RAMPercent > 100 {
					m.RAMPercent = 100
				}
			}
		} else if len(fields) >= 3 && strings.HasPrefix(fields[0], "Swap:") {
			total, err1 := strconv.ParseUint(fields[1], 10, 64)
			used, err2 := strconv.ParseUint(fields[2], 10, 64)
			if err1 == nil && err2 == nil {
				m.SwapTotalMB = total
				m.SwapUsedMB = used
			}
		}
	}
}

var diskPercentRegex = regexp.MustCompile(`(\d+)%`)

func parseDisk(body string, m *ServerMetrics) {
	lines := strings.Split(body, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 6 && !strings.HasPrefix(fields[0], "Filesystem") {
			total, err1 := strconv.ParseUint(fields[1], 10, 64)
			used, err2 := strconv.ParseUint(fields[2], 10, 64)
			if err1 == nil && err2 == nil && total > 0 {
				m.DiskTotalMB = total
				m.DiskUsedMB = used
				m.DiskPercent = float64(used) / float64(total) * 100
				if len(fields) >= 6 {
					m.DiskMount = fields[5]
				}
				break
			}
		}
	}
}

func parseCPU(body string, m *ServerMetrics) {
	// Look for %Cpu(s): ... id (idle)
	lines := strings.Split(body, "\n")
	for _, l := range lines {
		if strings.Contains(l, "Cpu(s):") || strings.Contains(l, "%Cpu") {
			parts := strings.Split(l, ",")
			for _, part := range parts {
				if strings.Contains(part, "id") {
					valStr := strings.TrimSpace(strings.ReplaceAll(part, "id", ""))
					fields := strings.Fields(valStr)
					if len(fields) > 0 {
						valStr = fields[len(fields)-1]
					}
					if idle, err := strconv.ParseFloat(valStr, 64); err == nil {
						usage := 100 - idle
						if usage < 0 {
							usage = 0
						}
						if usage > 100 {
							usage = 100
						}
						m.CPUPercent = usage
						return
					}
				}
			}
		}
	}

	// Fallback to Load1 if CPUPercent could not be parsed from top
	if m.CPUPercent == 0 && m.Load1 != "-" {
		if l1, err := strconv.ParseFloat(m.Load1, 64); err == nil {
			m.CPUPercent = l1 * 25 // approximate
			if m.CPUPercent > 100 {
				m.CPUPercent = 100
			}
		}
	}
}
