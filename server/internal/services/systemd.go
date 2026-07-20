// SPDX-License-Identifier: AGPL-3.0-only
package services

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	busName         = "org.freedesktop.systemd1"
	managerPath     = dbus.ObjectPath("/org/freedesktop/systemd1")
	managerIface    = "org.freedesktop.systemd1.Manager"
	unitIface       = "org.freedesktop.systemd1.Unit"
	propsIface      = "org.freedesktop.DBus.Properties"
	pollInterval    = 10 * time.Second
	listTimeout     = 15 * time.Second
	serviceSuffix   = ".service"
	signalBufSize   = 64
	subscriberSize  = 64
	maxUnitFileSize = 1 << 20
)

type Manager struct {
	conn *dbus.Conn

	watchOnce sync.Once
	broker    *changeBroker

	mu        sync.RWMutex
	state     map[string]Unit
	pathNames map[dbus.ObjectPath]string
}

func NewManager() *Manager {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return &Manager{broker: newChangeBroker()}
	}
	return &Manager{
		conn:      conn,
		broker:    newChangeBroker(),
		state:     map[string]Unit{},
		pathNames: map[dbus.ObjectPath]string{},
	}
}

func (m *Manager) Available() bool {
	return m != nil && m.conn != nil
}

func (m *Manager) List(ctx context.Context) ([]Unit, error) {
	if !m.Available() {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	return m.listUnits(ctx)
}

func (m *Manager) Detail(ctx context.Context, name string) (Detail, error) {
	if !m.Available() {
		return Detail{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	call := m.managerObject().CallWithContext(ctx, managerIface+".GetUnit", 0, name)
	if call.Err != nil {
		return Detail{}, fmt.Errorf("get unit: %w", call.Err)
	}
	if len(call.Body) == 0 {
		return Detail{}, fmt.Errorf("get unit: empty reply")
	}
	path, ok := call.Body[0].(dbus.ObjectPath)
	if !ok {
		return Detail{}, fmt.Errorf("get unit: unexpected reply shape")
	}
	propsCall := m.conn.Object(busName, path).CallWithContext(ctx, propsIface+".GetAll", 0, unitIface)
	if propsCall.Err != nil {
		return Detail{}, fmt.Errorf("get unit properties: %w", propsCall.Err)
	}
	if len(propsCall.Body) == 0 {
		return Detail{}, fmt.Errorf("get unit properties: empty reply")
	}
	props, ok := propsCall.Body[0].(map[string]dbus.Variant)
	if !ok {
		return Detail{}, fmt.Errorf("get unit properties: unexpected reply shape")
	}
	detail := Detail{
		Name:          name,
		Documentation: variantStrings(props["Documentation"]),
		Dependencies:  m.dependencies(ctx, props),
		Files:         unitFiles(props),
	}
	return detail, nil
}

func (m *Manager) dependencies(ctx context.Context, props map[string]dbus.Variant) []Dependency {
	seen := map[string]bool{}
	dependencies := []Dependency{}
	for _, relation := range []string{"Requires", "Wants"} {
		for _, path := range variantObjectPaths(props[relation]) {
			name := m.unitID(ctx, path)
			key := relation + "\x00" + name
			if name == "" || seen[key] {
				continue
			}
			seen[key] = true
			dependencies = append(dependencies, Dependency{Name: name, Relation: strings.ToLower(relation)})
		}
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Relation == dependencies[j].Relation {
			return dependencies[i].Name < dependencies[j].Name
		}
		return dependencies[i].Relation < dependencies[j].Relation
	})
	return dependencies
}

func unitFiles(props map[string]dbus.Variant) []UnitFile {
	paths := []struct {
		path     string
		override bool
	}{}
	if path := variantString(props["FragmentPath"]); path != "" {
		paths = append(paths, struct {
			path     string
			override bool
		}{path: path})
	}
	for _, path := range variantStrings(props["DropInPaths"]) {
		paths = append(paths, struct {
			path     string
			override bool
		}{path: path, override: true})
	}
	files := make([]UnitFile, 0, len(paths))
	for _, item := range paths {
		files = append(files, readUnitFile(item.path, item.override))
	}
	return files
}

func readUnitFile(path string, override bool) UnitFile {
	file := UnitFile{Path: path, Override: override}
	f, err := os.Open(path)
	if err != nil {
		file.Error = "Unable to read this unit file."
		return file
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxUnitFileSize+1))
	if err != nil {
		file.Error = "Unable to read this unit file."
		return file
	}
	if len(content) > maxUnitFileSize {
		file.Error = "This unit file is too large to display."
		return file
	}
	file.Content = string(content)
	return file
}

func variantStrings(v dbus.Variant) []string {
	if values, ok := v.Value().([]string); ok {
		return values
	}
	return []string{}
}

func variantObjectPaths(v dbus.Variant) []dbus.ObjectPath {
	if values, ok := v.Value().([]dbus.ObjectPath); ok {
		return values
	}
	return []dbus.ObjectPath{}
}

func (m *Manager) SubscribeChanges(ctx context.Context) (<-chan Unit, error) {
	if !m.Available() {
		return nil, ErrUnavailable
	}
	m.watchOnce.Do(func() {
		go m.watch()
	})
	ch := m.broker.subscribe()
	go func() {
		<-ctx.Done()
		m.broker.unsubscribe(ch)
	}()
	return ch, nil
}

func (m *Manager) managerObject() dbus.BusObject {
	return m.conn.Object(busName, managerPath)
}

func (m *Manager) listUnits(ctx context.Context) ([]Unit, error) {
	call := m.managerObject().CallWithContext(ctx, managerIface+".ListUnits", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("list units: %w", call.Err)
	}
	if len(call.Body) == 0 {
		return nil, fmt.Errorf("list units: empty reply")
	}
	raw, ok := call.Body[0].([][]interface{})
	if !ok {
		return nil, fmt.Errorf("list units: unexpected reply shape")
	}
	units := []Unit{}
	for _, fields := range raw {
		if len(fields) < 5 {
			continue
		}
		name, _ := fields[0].(string)
		if !strings.HasSuffix(name, serviceSuffix) {
			continue
		}
		u := Unit{Name: name}
		u.Description, _ = fields[1].(string)
		u.LoadState, _ = fields[2].(string)
		u.ActiveState, _ = fields[3].(string)
		u.SubState, _ = fields[4].(string)
		u.EnabledState = m.unitFileState(ctx, name)
		units = append(units, u)
	}
	return units, nil
}

func (m *Manager) unitFileState(ctx context.Context, name string) string {
	call := m.managerObject().CallWithContext(ctx, managerIface+".GetUnitFileState", 0, name)
	if call.Err != nil || len(call.Body) == 0 {
		return ""
	}
	state, _ := call.Body[0].(string)
	return state
}

func (m *Manager) unitID(ctx context.Context, path dbus.ObjectPath) string {
	m.mu.RLock()
	cached, ok := m.pathNames[path]
	m.mu.RUnlock()
	if ok {
		return cached
	}
	call := m.conn.Object(busName, path).CallWithContext(ctx, propsIface+".Get", 0, unitIface, "Id")
	if call.Err != nil || len(call.Body) == 0 {
		return ""
	}
	v, ok := call.Body[0].(dbus.Variant)
	if !ok {
		return ""
	}
	name, _ := v.Value().(string)
	if name != "" {
		m.mu.Lock()
		m.pathNames[path] = name
		m.mu.Unlock()
	}
	return name
}

func (m *Manager) watch() {
	ctx := context.Background()
	if units, err := m.listUnits(ctx); err == nil {
		m.mu.Lock()
		m.state = indexUnits(units)
		m.mu.Unlock()
	}
	if call := m.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.AddMatch", 0,
		"type='signal',sender='"+busName+"'"); call.Err != nil {
		return
	}
	signals := make(chan *dbus.Signal, signalBufSize)
	m.conn.Signal(signals)
	if call := m.managerObject().CallWithContext(ctx, managerIface+".Subscribe", 0); call.Err != nil {
		return
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case sig, ok := <-signals:
			if !ok || sig == nil {
				return
			}
			switch sig.Name {
			case propsIface + ".PropertiesChanged":
				m.handlePropertiesChanged(ctx, sig)
			case managerIface + ".UnitNew", managerIface + ".UnitRemoved":
				m.poll(ctx)
			}
		case <-ticker.C:
			m.poll(ctx)
		}
	}
}

func (m *Manager) poll(ctx context.Context) {
	pollCtx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	units, err := m.listUnits(pollCtx)
	if err != nil {
		return
	}
	m.mu.Lock()
	prev := m.state
	m.state = indexUnits(units)
	m.mu.Unlock()
	for _, d := range diffUnits(prev, units) {
		m.broker.publish(d)
	}
}

func (m *Manager) handlePropertiesChanged(ctx context.Context, sig *dbus.Signal) {
	if len(sig.Body) < 2 {
		return
	}
	iface, _ := sig.Body[0].(string)
	if iface != unitIface {
		return
	}
	changed, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}
	name := ""
	if v, ok := changed["Id"]; ok {
		name, _ = v.Value().(string)
	}
	if name == "" {
		name = m.unitID(ctx, sig.Path)
	}
	if name == "" || !strings.HasSuffix(name, serviceSuffix) {
		return
	}
	patch := Unit{Name: name}
	if v, ok := changed["Description"]; ok {
		patch.Description = variantString(v)
	}
	if v, ok := changed["LoadState"]; ok {
		patch.LoadState = variantString(v)
	}
	if v, ok := changed["ActiveState"]; ok {
		patch.ActiveState = variantString(v)
	}
	if v, ok := changed["SubState"]; ok {
		patch.SubState = variantString(v)
	}
	if patch.Description == "" && patch.LoadState == "" && patch.ActiveState == "" && patch.SubState == "" {
		return
	}
	m.mu.Lock()
	prev, known := m.state[name]
	if !known {
		prev = Unit{Name: name}
	}
	next := prev
	if patch.Description != "" {
		next.Description = patch.Description
	}
	if patch.LoadState != "" {
		next.LoadState = patch.LoadState
	}
	if patch.ActiveState != "" {
		next.ActiveState = patch.ActiveState
	}
	if patch.SubState != "" {
		next.SubState = patch.SubState
	}
	if m.state == nil {
		m.state = map[string]Unit{}
	}
	m.state[name] = next
	m.mu.Unlock()
	for _, d := range diffUnits(map[string]Unit{name: prev}, []Unit{next}) {
		m.broker.publish(d)
	}
}

func indexUnits(units []Unit) map[string]Unit {
	out := make(map[string]Unit, len(units))
	for _, u := range units {
		out[u.Name] = u
	}
	return out
}

func variantString(v dbus.Variant) string {
	if s, ok := v.Value().(string); ok {
		return s
	}
	return fmt.Sprint(v.Value())
}

type changeBroker struct {
	mu   sync.Mutex
	subs map[chan Unit]struct{}
}

func newChangeBroker() *changeBroker {
	return &changeBroker{subs: map[chan Unit]struct{}{}}
}

func (b *changeBroker) subscribe() chan Unit {
	ch := make(chan Unit, subscriberSize)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *changeBroker) unsubscribe(ch chan Unit) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *changeBroker) publish(u Unit) {
	b.mu.Lock()
	for ch := range b.subs {
		select {
		case ch <- u:
		default:
		}
	}
	b.mu.Unlock()
}
