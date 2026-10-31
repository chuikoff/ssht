package monitoring

import (
	"testing"
)

const sampleLinuxOutput = `
===UNAME===
Linux 6.1.0-rpi4-rpi-v7l armv7l

===UPTIME===
 10:45:12 up 14 days,  3:22,  2 users,  load average: 0.25, 0.45, 0.60

===MEM===
               total        used        free      shared  buff/cache   available
Mem:            7908        2372        3500          12        2036        5312
Swap:           2047         100        1947

===DISK===
Filesystem     1M-blocks  Used Available Use% Mounted on
/dev/root          29272 12450     15610  45% /

===CPU===
top - 10:45:12 up 14 days,  3:22,  2 users,  load average: 0.25, 0.45, 0.60
Tasks: 142 total,   1 running, 141 sleeping,   0 stopped,   0 zombie
%Cpu(s):  8.5 us,  2.1 sy,  0.0 ni, 89.0 id,  0.2 wa,  0.0 hi,  0.2 si,  0.0 st
`

func TestParseOutput(t *testing.T) {
	metrics := ParseOutput(sampleLinuxOutput)

	if metrics.OS != "Linux 6.1.0-rpi4-rpi-v7l armv7l" {
		t.Errorf("Unexpected OS: %s", metrics.OS)
	}

	if metrics.Uptime != "14 days,  3:22" {
		t.Errorf("Unexpected Uptime: %s", metrics.Uptime)
	}

	if metrics.Load1 != "0.25" || metrics.Load5 != "0.45" || metrics.Load15 != "0.60" {
		t.Errorf("Unexpected Loads: %s, %s, %s", metrics.Load1, metrics.Load5, metrics.Load15)
	}

	if metrics.RAMTotalMB != 7908 || metrics.RAMUsedMB != 2372 {
		t.Errorf("Unexpected RAM: %d / %d", metrics.RAMUsedMB, metrics.RAMTotalMB)
	}

	expectedRAMPercent := float64(2372) / float64(7908) * 100
	if metrics.RAMPercent < expectedRAMPercent-0.1 || metrics.RAMPercent > expectedRAMPercent+0.1 {
		t.Errorf("Unexpected RAM percent: %f", metrics.RAMPercent)
	}

	if metrics.DiskTotalMB != 29272 || metrics.DiskUsedMB != 12450 {
		t.Errorf("Unexpected Disk: %d / %d", metrics.DiskUsedMB, metrics.DiskTotalMB)
	}

	if metrics.CPUPercent < 10.9 || metrics.CPUPercent > 11.1 { // 100 - 89.0 = 11.0%
		t.Errorf("Unexpected CPU percent: %f", metrics.CPUPercent)
	}
}
