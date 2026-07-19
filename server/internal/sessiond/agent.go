// SPDX-License-Identifier: AGPL-3.0-only
package sessiond

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"lumio-os/server/internal/ipc"
)

const agentStateTimeout = 3 * time.Second

type agentProc struct {
	socketPath string
	cmd        *exec.Cmd

	mu   sync.Mutex
	dead bool
}

func (p *agentProc) alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.dead
}

func (p *agentProc) markDead() {
	p.mu.Lock()
	p.dead = true
	p.mu.Unlock()
}

func agentSocketPath(runDir string, uid uint32) string {
	return filepath.Join(runDir, "users", fmt.Sprintf("%d.sock", uid))
}

func (d *Daemon) ensureAgent(uid uint32) (string, error) {
	d.mu.Lock()
	proc, ok := d.agents[uid]
	d.mu.Unlock()
	if ok && proc.alive() {
		return proc.socketPath, nil
	}
	spawn := d.spawnAgentFn
	if spawn == nil {
		spawn = d.spawnAgent
	}
	proc, err := spawn(uid)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	d.agents[uid] = proc
	d.mu.Unlock()
	return proc.socketPath, nil
}

func (d *Daemon) spawnAgent(uid uint32) (*agentProc, error) {
	sockPath := agentSocketPath(d.cfg.RunDir, uid)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o750); err != nil {
		return nil, err
	}
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 {
		gid := int(-1)
		if g, err := groupID(agentSockGroup); err == nil {
			gid = int(g)
		}
		if err := os.Chown(sockPath, int(uid), gid); err != nil {
			_ = ln.Close()
			return nil, err
		}
	}
	if err := os.Chmod(sockPath, 0o660); err != nil {
		_ = ln.Close()
		return nil, err
	}
	lf, err := ln.(*net.UnixListener).File()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		_ = ln.Close()
		_ = lf.Close()
		return nil, err
	}
	gid64, _ := strconv.ParseUint(u.Gid, 10, 32)
	groups := supplementaryGroups(u.Username, uint32(gid64))

	exe, err := os.Executable()
	if err != nil {
		_ = ln.Close()
		_ = lf.Close()
		return nil, err
	}
	cmd := exec.Command(exe, "agent", "-fd", "3", "-broker-sock", brokerSocketPath(d.cfg.RunDir))
	cmd.Env = agentEnv(u)
	cmd.ExtraFiles = []*os.File{lf}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := setSpawnCredential(cmd, uid, uint32(gid64), groups); err != nil {
		_ = ln.Close()
		_ = lf.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = ln.Close()
		_ = lf.Close()
		return nil, err
	}
	_ = ln.Close()
	_ = lf.Close()
	proc := &agentProc{socketPath: sockPath, cmd: cmd}
	go func() {
		_ = cmd.Wait()
		proc.markDead()
	}()
	return proc, nil
}

func (d *Daemon) stopAgentIfIdle(uid uint32) {
	d.mu.Lock()
	proc, ok := d.agents[uid]
	d.mu.Unlock()
	if !ok || !proc.alive() {
		return
	}
	client := ipc.HTTPClient(proc.socketPath)
	resp, err := client.Get("http://agent/agent/state")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var state struct {
		Connections int `json:"connections"`
		PTYSessions int `json:"ptySessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return
	}
	if state.Connections == 0 && state.PTYSessions == 0 {
		_ = proc.cmd.Process.Signal(syscall.SIGTERM)
	}
}

func agentEnv(u *user.User) []string {
	shell := shellFor(u.Username)
	return []string{
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
		"SHELL=" + shell,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=C.UTF-8",
	}
}

func shellFor(username string) string {
	data, err := os.ReadFile("/etc/passwd")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 7 && fields[0] == username && fields[6] != "" {
				return fields[6]
			}
		}
	}
	return "/bin/sh"
}

func groupID(name string) (uint32, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	gid, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(gid), nil
}

func supplementaryGroups(username string, primaryGID uint32) []uint32 {
	groups := []uint32{primaryGID}
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return groups
	}
	seen := map[uint32]bool{primaryGID: true}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 4 {
			continue
		}
		for _, member := range strings.Split(fields[3], ",") {
			if member != username {
				continue
			}
			gid, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil || seen[uint32(gid)] {
				continue
			}
			seen[uint32(gid)] = true
			groups = append(groups, uint32(gid))
		}
	}
	return groups
}

func brokerSocketPath(runDir string) string {
	return filepath.Join(runDir, "broker.sock")
}

func serveUnix(ln net.Listener, handler http.Handler) error {
	return ipc.ServeUnix(ln, handler)
}
