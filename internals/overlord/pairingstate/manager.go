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

	"github.com/canonical/pebble/internals/overlord/state"
)

// Controller defines the available interface the pairing manager
// has to each controller.
type Controller interface {
	// PairingDisabled is called by the pairing manager when the pairing
	// window is disabled. This may happen before the controller expires the
	// window, e.g. when an incoming pairing request completes.
	PairingDisabled()
}

type pairingMode string

const (
	pairingModeDisabled pairingMode = "disabled"
	pairingModeSingle   pairingMode = "single"
	pairingModeMultiple pairingMode = "multiple"
)

type PairingManager struct {
	state       *state.State
	pairingMode pairingMode

	// The selected controller (or nil for no controller). The pairing manager
	// only supports a single pairing controller for now.
	controller Controller

	mu sync.Mutex
	// pairingWindow reflect the current state of the pairing window.
	pairingWindowOpen bool
	// isPaired is true if any pairing request has succeeded in the past. This
	// state must be persisted.
	isPaired bool
}

type Config struct {
	PairingMode pairingMode `json:"pairing-mode"`

	// Inlined JSON with custom unmarshaller.
	PairingControllerConfigs
}

func NewPairingManager(state *state.State, config *Config) (*PairingManager, error) {
	m := &PairingManager{
		state:       state,
		pairingMode: config.PairingMode,
	}
	// Create the controller specified in the configuration.
	controllers, err := config.PairingControllerConfigs.CreateControllers(m)
	if err != nil {
		return nil, err
	}
	// We only support a single controller right now.
	if len(controllers) > 1 {
		return nil, fmt.Errorf("cannot enable multiple pairing controllers: only supports a single controller at a time")
	}
	if len(controllers) == 1 {
		m.controller = controllers[0]
	}
	return m, nil
}

func (m *PairingManager) EnablePairing() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Pairing window already open.
	if m.pairingWindowOpen {
		return errors.New("cannot enable pairing: already open")
	}

	// Pairing conditions which disallows pairing.
	switch m.pairingMode {
	case pairingModeDisabled:
		return errors.New("cannot enable pairing: pairing disabled")
	case pairingModeSingle:
		if m.isPaired {
			return errors.New("cannot enable pairing: device already paired in single pairing mode")
		}
	case pairingModeMultiple:
	default:
		return fmt.Errorf("cannot enable pairing: unknown pairing mode %q", m.pairingMode)
	}

	m.pairingWindowOpen = true
	return nil
}

func (m *PairingManager) DisablePairing() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pairingWindowOpen = false
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *PairingManager) Ensure() error {
	return nil
}
