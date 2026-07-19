// SPDX-License-Identifier: AGPL-3.0-only
package system

import (
	"bufio"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CPUUsage struct {
	UsagePercent float64 `json:"usagePercent"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
	Cores        int     `json:"cores"`
}

type MemoryUsage struct {
	TotalBytes     uint64 `json:"totalBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type DiskUsage struct {
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

type NetUsage struct {
	Interface     string `json:"interface"`
	RxBytesPerSec uint64 `json:"rxBytesPerSec"`
	TxBytesPerSec uint64 `json:"txBytesPerSec"`
}

type Sample struct {
	TS      int64       `json:"ts"`
	CPU     CPUUsage    `json:"cpu"`
	Memory  MemoryUsage `json:"memory"`
	Disks   []DiskUsage `json:"disks"`
	Network []NetUsage  `json:"network"`
}

type cpuTimes struct {
	total uint64
	idle  uint64
}

type netTimes struct {
	rx uint64
	tx uint64
}

type Sampler struct {
	mu        sync.Mutex
	prevCPU   cpuTimes
	hasCPU    bool
	prevNet   map[string]netTimes
	prevNetAt time.Time
}

func NewSampler() *Sampler {
	return &Sampler{prevNet: map[string]netTimes{}}
}

func (s *Sampler) Sample() Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := Sample{
		TS:      now.UnixMilli(),
		Disks:   []DiskUsage{},
		Network: []NetUsage{},
	}
	out.CPU.Cores = runtime.NumCPU()
	if l1, l5, l15, ok := readLoadavg(); ok {
		out.CPU.Load1, out.CPU.Load5, out.CPU.Load15 = l1, l5, l15
	}
	if cur, ok := readCPUTimes(); ok {
		if s.hasCPU {
			out.CPU.UsagePercent = cpuUsagePercent(s.prevCPU, cur)
		}
		s.prevCPU = cur
		s.hasCPU = true
	}
	if total, available, ok := readMeminfo(); ok {
		out.Memory.TotalBytes = total
		out.Memory.AvailableBytes = available
		if total >= available {
			out.Memory.UsedBytes = total - available
		}
	}
	for _, mount := range realMounts() {
		total, free, err := statfs(mount)
		if err != nil || total == 0 {
			continue
		}
		used := uint64(0)
		if total >= free {
			used = total - free
		}
		out.Disks = append(out.Disks, DiskUsage{Mount: mount, TotalBytes: total, UsedBytes: used})
	}
	if cur, ok := readNetDev(); ok {
		if !s.prevNetAt.IsZero() {
			dt := now.Sub(s.prevNetAt).Seconds()
			if dt > 0 {
				names := make([]string, 0, len(cur))
				for name := range cur {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					prev, seen := s.prevNet[name]
					if !seen {
						continue
					}
					out.Network = append(out.Network, NetUsage{
						Interface:     name,
						RxBytesPerSec: rate(prev.rx, cur[name].rx, dt),
						TxBytesPerSec: rate(prev.tx, cur[name].tx, dt),
					})
				}
			}
		}
		s.prevNet = cur
		s.prevNetAt = now
	}
	return out
}

func rate(prev, cur uint64, dt float64) uint64 {
	if cur < prev {
		return 0
	}
	return uint64(math.Round(float64(cur-prev) / dt))
}

func cpuUsagePercent(prev, cur cpuTimes) float64 {
	if cur.total <= prev.total {
		return 0
	}
	dTotal := cur.total - prev.total
	dIdle := uint64(0)
	if cur.idle >= prev.idle {
		dIdle = cur.idle - prev.idle
	}
	if dIdle >= dTotal {
		return 0
	}
	return math.Round(float64(dTotal-dIdle)/float64(dTotal)*1000) / 10
}

func readCPUTimes() (cpuTimes, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	return parseCPUTimes(string(data))
}

func parseCPUTimes(content string) (cpuTimes, bool) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return cpuTimes{}, false
		}
		var t cpuTimes
		for i, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuTimes{}, false
			}
			t.total += v
			if i == 3 || i == 4 {
				t.idle += v
			}
		}
		return t, true
	}
	return cpuTimes{}, false
}

func readLoadavg() (float64, float64, float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, false
	}
	return parseLoadavg(strings.TrimSpace(string(data)))
}

func parseLoadavg(line string) (float64, float64, float64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	l1, err1 := strconv.ParseFloat(fields[0], 64)
	l5, err5 := strconv.ParseFloat(fields[1], 64)
	l15, err15 := strconv.ParseFloat(fields[2], 64)
	if err1 != nil || err5 != nil || err15 != nil {
		return 0, 0, 0, false
	}
	return l1, l5, l15, true
}

func readMeminfo() (total uint64, available uint64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	return parseMeminfo(string(data))
}

func parseMeminfo(content string) (uint64, uint64, bool) {
	var total, available uint64
	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		key, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		var target *uint64
		var have *bool
		switch strings.TrimSpace(key) {
		case "MemTotal":
			target, have = &total, &haveTotal
		case "MemAvailable":
			target, have = &available, &haveAvail
		default:
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		*target = v * 1024
		*have = true
	}
	return total, available, haveTotal && haveAvail
}

func readNetDev() (map[string]netTimes, bool) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, false
	}
	return parseNetDev(string(data)), true
}

func parseNetDev(content string) map[string]netTimes {
	out := map[string]netTimes{}
	for _, line := range strings.Split(content, "\n") {
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, errRx := strconv.ParseUint(fields[0], 10, 64)
		tx, errTx := strconv.ParseUint(fields[8], 10, 64)
		if errRx != nil || errTx != nil {
			continue
		}
		out[name] = netTimes{rx: rx, tx: tx}
	}
	return out
}

func realMounts() []string {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return []string{"/"}
	}
	mounts := parseMounts(string(data))
	if len(mounts) == 0 {
		return []string{"/"}
	}
	return mounts
}

func parseMounts(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		device, mount := fields[0], fields[1]
		if !strings.HasPrefix(device, "/dev/") || strings.HasPrefix(device, "/dev/loop") {
			continue
		}
		if seen[mount] {
			continue
		}
		seen[mount] = true
		out = append(out, mount)
	}
	return out
}
