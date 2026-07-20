// SPDX-License-Identifier: AGPL-3.0-only
package network

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	DefaultConfirmTimeout = 90
	MinConfirmTimeout     = 30
	MaxConfirmTimeout     = 300
	netplanBusName        = "io.netplan.Netplan"
	netplanRootPath       = dbus.ObjectPath("/io/netplan/Netplan")
	netplanRootIface      = "io.netplan.Netplan"
	netplanConfigIface    = "io.netplan.Netplan.Config"
	netplanOriginHint     = "90-lumio"
)

var (
	ErrBusy       = errors.New("another network change is pending")
	ErrExpired    = errors.New("network confirmation expired")
	ErrToken      = errors.New("network confirmation token is invalid")
	ErrValidation = errors.New("network configuration is invalid")
	interfaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,14}$`)
	revisionValue = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Config struct {
	Version   int                       `json:"version"`
	Ethernets map[string]EthernetConfig `json:"ethernets"`
}

type EthernetConfig struct {
	DHCP4       bool         `json:"dhcp4"`
	DHCP6       bool         `json:"dhcp6"`
	Addresses   []string     `json:"addresses,omitempty"`
	Nameservers *Nameservers `json:"nameservers,omitempty"`
	Routes      []Route      `json:"routes,omitempty"`
	Optional    bool         `json:"optional,omitempty"`
}

type Nameservers struct {
	Addresses []string `json:"addresses,omitempty"`
	Search    []string `json:"search,omitempty"`
}

type Route struct {
	To     string `json:"to"`
	Via    string `json:"via"`
	Metric uint32 `json:"metric,omitempty"`
}

type Interface struct {
	Name         string   `json:"name"`
	HardwareAddr string   `json:"hardwareAddress,omitempty"`
	Addresses    []string `json:"addresses"`
	Up           bool     `json:"up"`
	Loopback     bool     `json:"loopback"`
}

type Snapshot struct {
	Revision   string      `json:"revision"`
	Interfaces []Interface `json:"interfaces"`
}

type Pending struct {
	Token            string    `json:"token"`
	PreviousRevision string    `json:"previousRevision"`
	ExpiresAt        time.Time `json:"expiresAt"`
	ConfirmTimeout   int       `json:"confirmTimeoutSec"`
}

type StaleError struct {
	Expected string
	Actual   string
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("network revision changed: expected %s, got %s", e.Expected, e.Actual)
}

type Snapshotter interface {
	Available() bool
	Snapshot(ctx context.Context) (Snapshot, error)
}

type backend interface {
	create(ctx context.Context) (dbus.ObjectPath, string, error)
	set(ctx context.Context, path dbus.ObjectPath, delta, originHint string) error
	try(ctx context.Context, path dbus.ObjectPath, timeoutSec uint32) error
	apply(ctx context.Context, path dbus.ObjectPath) error
	cancel(ctx context.Context, path dbus.ObjectPath) error
}

type dbusBackend struct {
	conn *dbus.Conn
}

func newDBusBackend() (*dbusBackend, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &dbusBackend{conn: conn}, nil
}

func (b *dbusBackend) create(ctx context.Context) (dbus.ObjectPath, string, error) {
	root := b.conn.Object(netplanBusName, netplanRootPath)
	var path dbus.ObjectPath
	if err := root.CallWithContext(ctx, netplanRootIface+".Config", 0).Store(&path); err != nil {
		return "", "", fmt.Errorf("create Netplan configuration: %w", err)
	}
	var merged string
	if err := b.conn.Object(netplanBusName, path).CallWithContext(ctx, netplanConfigIface+".Get", 0).Store(&merged); err != nil {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = b.cancel(cancelCtx, path)
		return "", "", fmt.Errorf("read Netplan configuration: %w", err)
	}
	return path, merged, nil
}

func (b *dbusBackend) set(ctx context.Context, path dbus.ObjectPath, delta, originHint string) error {
	return boolCall(ctx, b.conn.Object(netplanBusName, path), netplanConfigIface+".Set", delta, originHint)
}

func (b *dbusBackend) try(ctx context.Context, path dbus.ObjectPath, timeoutSec uint32) error {
	return boolCall(ctx, b.conn.Object(netplanBusName, path), netplanConfigIface+".Try", timeoutSec)
}

func (b *dbusBackend) apply(ctx context.Context, path dbus.ObjectPath) error {
	return boolCall(ctx, b.conn.Object(netplanBusName, path), netplanConfigIface+".Apply")
}

func (b *dbusBackend) cancel(ctx context.Context, path dbus.ObjectPath) error {
	return boolCall(ctx, b.conn.Object(netplanBusName, path), netplanConfigIface+".Cancel")
}

func boolCall(ctx context.Context, object dbus.BusObject, method string, args ...any) error {
	var ok bool
	if err := object.CallWithContext(ctx, method, 0, args...).Store(&ok); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if !ok {
		return fmt.Errorf("%s was rejected", method)
	}
	return nil
}

type Reader struct {
	backend backend
}

func NewReader() *Reader {
	backend, err := newDBusBackend()
	if err != nil {
		return &Reader{}
	}
	return &Reader{backend: backend}
}

func (r *Reader) Available() bool {
	return r != nil && r.backend != nil
}

func (r *Reader) Snapshot(ctx context.Context) (Snapshot, error) {
	if !r.Available() {
		return Snapshot{}, errors.New("Netplan D-Bus is unavailable")
	}
	path, merged, err := r.backend.create(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.backend.cancel(cancelCtx, path)
	}()
	interfaces, err := liveInterfaces()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: Revision(merged), Interfaces: interfaces}, nil
}

type Controller struct {
	backend backend
	mu      sync.Mutex
	pending *pendingState
}

type pendingState struct {
	Pending
	path  dbus.ObjectPath
	timer *time.Timer
}

func NewController() (*Controller, error) {
	backend, err := newDBusBackend()
	if err != nil {
		return nil, err
	}
	return newController(backend), nil
}

func newController(backend backend) *Controller {
	return &Controller{backend: backend}
}

func (c *Controller) Apply(ctx context.Context, config Config, expectedRevision string, timeoutSec int) (Pending, error) {
	if err := ValidateConfig(config); err != nil {
		return Pending{}, err
	}
	if !ValidRevision(expectedRevision) {
		return Pending{}, fmt.Errorf("%w: expected revision is required", ErrValidation)
	}
	if timeoutSec == 0 {
		timeoutSec = DefaultConfirmTimeout
	}
	if timeoutSec < MinConfirmTimeout || timeoutSec > MaxConfirmTimeout {
		return Pending{}, fmt.Errorf("%w: confirm timeout must be between %d and %d seconds", ErrValidation, MinConfirmTimeout, MaxConfirmTimeout)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != nil {
		if time.Now().Before(c.pending.ExpiresAt) {
			return Pending{}, ErrBusy
		}
		if err := c.rollbackAndVerify(ctx, c.pending); err != nil {
			return Pending{}, fmt.Errorf("verify previous network configuration: %w", err)
		}
		c.pending.timer.Stop()
		c.pending = nil
	}

	path, merged, err := c.backend.create(ctx)
	if err != nil {
		return Pending{}, err
	}
	cancelCandidate := true
	defer func() {
		if cancelCandidate {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = c.backend.cancel(cancelCtx, path)
		}
	}()
	actualRevision := Revision(merged)
	if actualRevision != expectedRevision {
		return Pending{}, &StaleError{Expected: expectedRevision, Actual: actualRevision}
	}

	names := make([]string, 0, len(config.Ethernets))
	for name := range config.Ethernets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		encoded, err := json.Marshal(config.Ethernets[name])
		if err != nil {
			return Pending{}, err
		}
		delta := "ethernets." + name + "=" + string(encoded)
		if err := c.backend.set(ctx, path, delta, netplanOriginHint); err != nil {
			return Pending{}, err
		}
	}
	if err := c.backend.try(ctx, path, uint32(timeoutSec)); err != nil {
		return Pending{}, err
	}
	token, err := randomToken()
	if err != nil {
		return Pending{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(timeoutSec) * time.Second)
	state := &pendingState{
		Pending: Pending{
			Token:            token,
			PreviousRevision: actualRevision,
			ExpiresAt:        expiresAt,
			ConfirmTimeout:   timeoutSec,
		},
		path: path,
	}
	state.timer = time.AfterFunc(time.Until(expiresAt), func() {
		c.expire(token, path)
	})
	c.pending = state
	cancelCandidate = false
	return state.Pending, nil
}

func (c *Controller) Confirm(ctx context.Context, token string) (Pending, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		return Pending{}, ErrExpired
	}
	if token == "" || token != c.pending.Token {
		return Pending{}, ErrToken
	}
	if !time.Now().Before(c.pending.ExpiresAt) {
		return Pending{}, ErrExpired
	}
	if err := c.backend.apply(ctx, c.pending.path); err != nil {
		return Pending{}, err
	}
	result := c.pending.Pending
	c.pending.timer.Stop()
	c.pending = nil
	return result, nil
}

func (c *Controller) expire(token string, path dbus.ObjectPath) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil || c.pending.Token != token || c.pending.path != path {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.rollbackAndVerify(ctx, c.pending); err != nil {
		c.pending.timer = time.AfterFunc(5*time.Second, func() {
			c.expire(token, path)
		})
		return
	}
	c.pending.timer.Stop()
	c.pending = nil
}

func (c *Controller) rollbackAndVerify(ctx context.Context, pending *pendingState) error {
	cancelErr := c.backend.cancel(ctx, pending.path)
	checkPath, merged, checkErr := c.backend.create(ctx)
	if checkErr != nil {
		if cancelErr != nil {
			return errors.Join(cancelErr, checkErr)
		}
		return checkErr
	}
	defer func() { _ = c.backend.cancel(ctx, checkPath) }()
	if Revision(merged) != pending.PreviousRevision {
		if cancelErr != nil {
			return errors.Join(cancelErr, errors.New("rollback revision does not match the previous configuration"))
		}
		return errors.New("rollback revision does not match the previous configuration")
	}
	return nil
}

func ValidateConfig(config Config) error {
	if config.Version != 2 {
		return fmt.Errorf("%w: version must be 2", ErrValidation)
	}
	if len(config.Ethernets) == 0 || len(config.Ethernets) > 8 {
		return fmt.Errorf("%w: between 1 and 8 ethernet interfaces are required", ErrValidation)
	}
	for name, ethernet := range config.Ethernets {
		if !interfaceName.MatchString(name) {
			return fmt.Errorf("%w: invalid interface name", ErrValidation)
		}
		if !ethernet.DHCP4 && !ethernet.DHCP6 && len(ethernet.Addresses) == 0 {
			return fmt.Errorf("%w: %s needs DHCP or a static address", ErrValidation, name)
		}
		if len(ethernet.Addresses) > 8 {
			return fmt.Errorf("%w: %s has too many addresses", ErrValidation, name)
		}
		seen := map[string]bool{}
		for _, value := range ethernet.Addresses {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.String() != value || seen[value] {
				return fmt.Errorf("%w: %s has an invalid or duplicate address", ErrValidation, name)
			}
			seen[value] = true
		}
		if ethernet.Nameservers != nil {
			if len(ethernet.Nameservers.Addresses) > 6 || len(ethernet.Nameservers.Search) > 6 {
				return fmt.Errorf("%w: %s has too many DNS values", ErrValidation, name)
			}
			for _, value := range ethernet.Nameservers.Addresses {
				if _, err := netip.ParseAddr(value); err != nil {
					return fmt.Errorf("%w: %s has an invalid DNS address", ErrValidation, name)
				}
			}
			for _, value := range ethernet.Nameservers.Search {
				if !validDomain(value) {
					return fmt.Errorf("%w: %s has an invalid DNS search domain", ErrValidation, name)
				}
			}
		}
		if len(ethernet.Routes) > 16 {
			return fmt.Errorf("%w: %s has too many routes", ErrValidation, name)
		}
		for _, route := range ethernet.Routes {
			if route.To != "default" {
				if _, err := netip.ParsePrefix(route.To); err != nil {
					return fmt.Errorf("%w: %s has an invalid route destination", ErrValidation, name)
				}
			}
			if _, err := netip.ParseAddr(route.Via); err != nil {
				return fmt.Errorf("%w: %s has an invalid route gateway", ErrValidation, name)
			}
		}
	}
	return nil
}

func ValidRevision(value string) bool {
	return revisionValue.MatchString(value)
}

func Revision(merged string) string {
	sum := sha256.Sum256([]byte(merged))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func liveInterfaces() ([]Interface, error) {
	values, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := make([]Interface, 0, len(values))
	for _, value := range values {
		addresses, err := value.Addrs()
		if err != nil {
			return nil, err
		}
		formatted := make([]string, 0, len(addresses))
		for _, address := range addresses {
			formatted = append(formatted, address.String())
		}
		sort.Strings(formatted)
		result = append(result, Interface{
			Name:         value.Name,
			HardwareAddr: value.HardwareAddr.String(),
			Addresses:    formatted,
			Up:           value.Flags&net.FlagUp != 0,
			Loopback:     value.Flags&net.FlagLoopback != 0,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func validDomain(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
