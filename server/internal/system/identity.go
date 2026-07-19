// SPDX-License-Identifier: AGPL-3.0-only
package system

import (
	"os"
	"runtime"
	"strings"
	"time"
)

type OSInfo struct {
	ID         string `json:"id"`
	VersionID  string `json:"versionId"`
	PrettyName string `json:"prettyName"`
	Kernel     string `json:"kernel"`
}

type Identity struct {
	Hostname     string `json:"hostname"`
	OS           OSInfo `json:"os"`
	Architecture string `json:"architecture"`
	BootID       string `json:"bootId"`
	ServerTime   string `json:"serverTime"`
}

func ReadIdentity() Identity {
	kv := readOSRelease()
	id := kv["ID"]
	if id == "" {
		id = runtime.GOOS
	}
	pretty := kv["PRETTY_NAME"]
	if pretty == "" {
		pretty = id
	}
	hostname, _ := os.Hostname()
	return Identity{
		Hostname: hostname,
		OS: OSInfo{
			ID:         id,
			VersionID:  kv["VERSION_ID"],
			PrettyName: pretty,
			Kernel:     kernelRelease(),
		},
		Architecture: machineArch(),
		BootID:       readBootID(),
		ServerTime:   time.Now().UTC().Format(time.RFC3339),
	}
}

func readOSRelease() map[string]string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		data, err := os.ReadFile(path)
		if err == nil {
			return parseOSRelease(string(data))
		}
	}
	return map[string]string{}
}

func parseOSRelease(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		out[key] = value
	}
	return out
}

func kernelRelease() string {
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}
	out, err := runCommand(2*time.Second, "uname", "-r")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func machineArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	case "arm":
		return "armv7l"
	default:
		return runtime.GOARCH
	}
}

func readBootID() string {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
