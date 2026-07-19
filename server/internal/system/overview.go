// SPDX-License-Identifier: AGPL-3.0-only
package system

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Overview struct {
	UptimeSeconds          uint64  `json:"uptimeSeconds"`
	CPUUsagePercent        float64 `json:"cpuUsagePercent"`
	MemoryUsedBytes        uint64  `json:"memoryUsedBytes"`
	MemoryTotalBytes       uint64  `json:"memoryTotalBytes"`
	UpdatesPending         int     `json:"updatesPending"`
	SecurityUpdatesPending int     `json:"securityUpdatesPending"`
	FailedUnits            int     `json:"failedUnits"`
	RebootRequired         bool    `json:"rebootRequired"`
}

func CollectOverview(failedUnits int) Overview {
	o := Overview{FailedUnits: failedUnits}
	o.UptimeSeconds = readUptime()
	o.CPUUsagePercent = measureCPUUsage(150 * time.Millisecond)
	if total, available, ok := readMeminfo(); ok {
		o.MemoryTotalBytes = total
		if total >= available {
			o.MemoryUsedBytes = total - available
		}
	}
	o.UpdatesPending, o.SecurityUpdatesPending = readAptCheck()
	o.RebootRequired = rebootRequired()
	return o
}

func readUptime() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return uint64(seconds)
}

func measureCPUUsage(window time.Duration) float64 {
	first, ok := readCPUTimes()
	if !ok {
		return 0
	}
	time.Sleep(window)
	second, ok := readCPUTimes()
	if !ok {
		return 0
	}
	return cpuUsagePercent(first, second)
}

var aptCheckLine = regexp.MustCompile(`(?m)^\s*(\d+);(\d+)\s*$`)

func readAptCheck() (updates int, security int) {
	const aptCheck = "/usr/lib/update-notifier/apt-check"
	if _, err := os.Stat(aptCheck); err != nil {
		return 0, 0
	}
	out, err := combinedCommand(15*time.Second, aptCheck)
	if err != nil && out == "" {
		return 0, 0
	}
	match := aptCheckLine.FindStringSubmatch(out)
	if match == nil {
		return 0, 0
	}
	updates, _ = strconv.Atoi(match[1])
	security, _ = strconv.Atoi(match[2])
	return updates, security
}

func rebootRequired() bool {
	_, err := os.Stat("/var/run/reboot-required")
	return err == nil
}
