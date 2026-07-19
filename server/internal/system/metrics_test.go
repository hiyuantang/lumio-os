// SPDX-License-Identifier: AGPL-3.0-only
package system

import (
	"testing"
)

func TestParseCPUTimes(t *testing.T) {
	content := "cpu  4705 356 584 3699 23 23 0 0 0 0\ncpu0 2500 100 300 1800 10 10 0 0 0 0\n"
	times, ok := parseCPUTimes(content)
	if !ok {
		t.Fatal("parseCPUTimes failed")
	}
	wantTotal := uint64(4705 + 356 + 584 + 3699 + 23 + 23)
	if times.total != wantTotal {
		t.Errorf("total = %d, want %d", times.total, wantTotal)
	}
	wantIdle := uint64(3699 + 23)
	if times.idle != wantIdle {
		t.Errorf("idle = %d, want %d", times.idle, wantIdle)
	}
}

func TestCPUUsagePercent(t *testing.T) {
	prev := cpuTimes{total: 1000, idle: 800}
	cur := cpuTimes{total: 1100, idle: 850}
	got := cpuUsagePercent(prev, cur)
	if got != 50.0 {
		t.Errorf("usage = %v, want 50.0", got)
	}
	if got := cpuUsagePercent(cur, cur); got != 0 {
		t.Errorf("zero delta usage = %v, want 0", got)
	}
	if got := cpuUsagePercent(cpuTimes{total: 0, idle: 0}, cpuTimes{total: 0, idle: 0}); got != 0 {
		t.Errorf("zero total usage = %v, want 0", got)
	}
}

func TestParseLoadavg(t *testing.T) {
	l1, l5, l15, ok := parseLoadavg("0.42 0.38 0.31 2/101 1234")
	if !ok {
		t.Fatal("parseLoadavg failed")
	}
	if l1 != 0.42 || l5 != 0.38 || l15 != 0.31 {
		t.Errorf("got %v %v %v", l1, l5, l15)
	}
	if _, _, _, ok := parseLoadavg("garbage"); ok {
		t.Error("expected failure on garbage")
	}
}

func TestParseMeminfo(t *testing.T) {
	content := "MemTotal:       16384000 kB\nMemFree:         8192000 kB\nMemAvailable:   12288000 kB\nBuffers:          102400 kB\n"
	total, available, ok := parseMeminfo(content)
	if !ok {
		t.Fatal("parseMeminfo failed")
	}
	if total != 16384000*1024 {
		t.Errorf("total = %d", total)
	}
	if available != 12288000*1024 {
		t.Errorf("available = %d", available)
	}
	if _, _, ok := parseMeminfo("MemFree: 1 kB\n"); ok {
		t.Error("expected failure without required fields")
	}
}

func TestParseNetDev(t *testing.T) {
	content := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:     100       1    0    0    0     0          0         0      200       1    0    0    0     0       0          0
  eth0:   15234      10    0    0    0     0          0         0     8211       9    0    0    0     0       0          0
`
	devs := parseNetDev(content)
	if _, ok := devs["lo"]; ok {
		t.Error("loopback should be skipped")
	}
	eth, ok := devs["eth0"]
	if !ok {
		t.Fatal("eth0 missing")
	}
	if eth.rx != 15234 || eth.tx != 8211 {
		t.Errorf("eth0 = %+v", eth)
	}
}

func TestParseMounts(t *testing.T) {
	content := `sysfs /sys sysfs rw,nosuid,nodev,noexec,relatime 0 0
/dev/sda1 / ext4 rw,relatime 0 0
/dev/sdb1 /data xfs rw,relatime 0 0
/dev/loop0 /snap/core/123 squashfs ro 0 0
tmpfs /run tmpfs rw 0 0
/dev/sda1 / ext4 rw,relatime 0 0
`
	mounts := parseMounts(content)
	if len(mounts) != 2 || mounts[0] != "/" || mounts[1] != "/data" {
		t.Errorf("mounts = %v", mounts)
	}
}
