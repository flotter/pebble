// Copyright (c) 2025 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package pairingstate

import (
	"errors"
	"fmt"
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// Controller defines the interface the pairing manager has to a pairing
// controller.
type Controller interface {
	// Type reports the pairing controller type. This matches the string
	// key with which the controller extension is registered. Type may
	// not call PairingAccessor methods.
	Type() string

	// Config returns the controller configuration. Config may not call
	// PairingAccessor methods.
	Config() ControllerConfig

	// PairingDisabled informs the controller when the pairing window
	// is closed by the pairing manager. PairingDisabled may not call
	// PairingAccessor methods.
	PairingDisabled(reason DisableReason)

	// EnsureConfig supplies a configuration to the controller, or updates
	// an existing configuration if the controller already has a
	// configuration. If the configuration is nil, the controller must
	// shut down and free all its resources. EnsureConfig may call
	// PairingAccessor methods.
	EnsureConfig(config ControllerConfig) error
}

type DisableReason int

const (
	// Pairing window disabled due to an internal error
	disableInternalError DisableReason = iota
	// Pairing window disabled following a successful
	// pairing request.
	disablePairingSuccess
	// Pairing window disabled following a failed
	// pairing request.
	disablePairingFailure
	// Pairing window disabled following a change in
	// pairing manager configuration.
	disablePairingReconfig
)

func (d DisableReason) String() string {
	switch d {
	case disableInternalError:
		return "internal error"
	case disablePairingSuccess:
		return "pairing success"
	case disablePairingFailure:
		return "pairing failure"
	case disablePairingReconfig:
		return "config changed"
	default:
		return "unknown"
	}
}

// Mode controls the pairing policy of the pairing manager.
type mode string

const (
	modeDisabled mode = "disabled"
	modeSingle   mode = "single"
	modeMultiple mode = "multiple"
)

type ManagerConfig struct {
	Mode mode `yaml:"mode"`
}

func (c ManagerConfig) validate() error {
	switch c.Mode {
	case "", modeDisabled, modeSingle, modeMultiple:
	default:
		return fmt.Errorf("cannot support pairing manager mode %q: unknown mode", c.Mode)
	}
	return nil
}

func (c ManagerConfig) copy() ManagerConfig {
	copied := c
	return copied
}

func (c *ManagerConfig) merge(other ManagerConfig) {
	if other.Mode != "" {
		c.Mode = other.Mode
	}
}

func (c ManagerConfig) Equal(other ManagerConfig) bool {
	return c.Mode == other.Mode
}

type PairingManager struct {
	state *state.State
	mu    sync.Mutex
	// Controller specifies the configured pairing controller.
	controller Controller
	// Config represents the pairing manager configuration.
	config ManagerConfig
	// IsPaired is true if any pairing request has succeeded in the past.
	isPaired bool
	// windowOpen reflects the pairing manager's view of the state of the
	// pairing window. When the pairing window is open, incoming TLS
	// pairing requests are permitted, and the pairing end-point is made
	// accessible.
	windowOpen bool
	// planChangedMutex forces pairing configuration (including
	// controller changes) from the plan to be serialised, without relying
	// on the global manager lock. This is needed to allow the Controller
	// interface EnsureConfig method access to the PairingAcessor without
	// a deadlock.
	planChangedMutex sync.Mutex
}

func NewPairingManager(state *state.State) *PairingManager {
	m := &PairingManager{
		state:  state,
	}
	m.config.Mode = modeDisabled
	return m
}

// Ensure implements StateManager.Ensure.
func (m *PairingManager) Ensure() error {
	return nil
}

// PlanChanged informs the pairing manager that the plan has been updated.
func (m *PairingManager) PlanChanged(newPlan *plan.Plan) {
	m.planChangedMutex.Lock()
	defer m.planChangedMutex.Unlock()

	var currController Controller
	var prevController Controller
	var newConfig *PairingConfig

	defer func() {
		if prevController != nil {
			// Shutdown controller.
			err := prevController.EnsureConfig(nil)
			if err != nil {
				logger.Noticef("Cannot shutdown pairing controller %q: %s", prevController.Type(), err)
			}
		}
		if currController != nil {
			// Apply controller with new configuration.
			err := currController.EnsureConfig(newConfig.Controller.Config)
			if err != nil {
				logger.Noticef("Cannot apply configuration for pairing controller %q: %s", prevController.Type(), err)
			}
		}
	}()

	m.mu.Lock()
	defer m.mu.Unlock()

	newConfig = newPlan.Sections[PairingField].(*PairingConfig)

	// If the configuration is changing in any way and the pairing window
	// is open, we close it as a security precuation.
	if m.windowOpen && m.controller != nil && m.configChanged(newConfig) {
		m.windowOpen = false
		m.controller.PairingDisabled(disablePairingReconfig)
	}

	// Update the pairing manager configuration if needed.
	if !m.config.Equal(newConfig.Config) {
		m.config = newConfig.Config
	}

	// Is the current controller getting replaced by a new type?
	if m.controller != nil {
		if m.controller.Type() != newConfig.Controller.Type {
			prevController = m.controller
			m.controller = nil
		}
	}

	// Do we need to create a new controller type?
	if m.controller == nil && newConfig.Controller.Type != "" {
		ext := controllerExtensions[newConfig.Controller.Type]
		m.controller = ext.NewController(m)
	}
	currController = m.controller
}

// configChanged returns true if the pairing manager or pairing controller
// configuration changed.
func (m *PairingManager) configChanged(c *PairingConfig) bool {
	if !m.config.Equal(c.Config) {
		return false
	}
	if m.controller.Type() != c.Controller.Type {
		return false
	}
	if !c.Controller.Config.Equal(m.controller.Config()) {
		return false
	}
	return true
}

func (m *PairingManager) EnablePairing() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.windowOpen {
		return errors.New("cannot enable pairing: already enabled")
	}
	switch m.config.Mode {
	case modeDisabled:
		return errors.New("cannot enable pairing: pairing not allowed")
	case modeSingle:
		if m.isPaired {
			return errors.New("cannot enable pairing: device already paired and pairing-mode is 'single'")
		}
	case modeMultiple:
	default:
		return fmt.Errorf("cannot enable pairing: unknown pairing mode %q", m.config.Mode)
	}
	m.windowOpen = true
	return nil
}

func (m *PairingManager) DisablePairing() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.windowOpen {
		return errors.New("cannot disable pairing: already disabled")
	}

	m.windowOpen = false
	return nil
}
