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
	"encoding/json"
	"fmt"
)

var (
	// Registered pairing controller extensions.
	controllerExtensions = map[string]ControllerExtension{}
)

// PairingAccessor provides access to the pairing manager functions to
// control the pairing window.
type PairingAccessor interface {
	EnablePairing() error
	DisablePairing() error
}

type ControllerConfig = any

type ControllerExtension interface {
	// UnmarshalConfig unmarshals the controller configuration JSON into
	// a concrete backing type, and returns a pointer to the instance.
	UnmarshalConfig(json.RawMessage) (ControllerConfig, error)

	// NewController creates a new pairing manager controller.
	NewController(PairingAccessor, ControllerConfig) (Controller, error)
}

// RegisterPairingController registers a pairing controller extension.
func RegisterPairingController(name string, ext ControllerExtension) {
	if _, ok := controllerExtensions[name]; ok {
		panic(fmt.Sprintf("internal error: controller %q already registered", name))
	}
	controllerExtensions[name] = ext
}

// UnregisterPairingController removed a registered pairing controller extension.
func UnregisterPairingController(name string) {
	delete(controllerExtensions, name)
}

type PairingControllerConfigs struct {
	PairingControllers map[string]ControllerConfig `json:"pairing-controllers"`
}

func (c *PairingControllerConfigs) UnmarshalJSON(data []byte) error {
	var rawCfg struct {
		PairingControllers map[string]json.RawMessage `json:"pairing-controllers"`
	}
	if err := json.Unmarshal(data, &rawCfg); err != nil {
		return err
	}

	c.PairingControllers = make(map[string]ControllerConfig)
	for name, rawJSON := range rawCfg.PairingControllers {
		ext, ok := controllerExtensions[name]
		if !ok {
			// Ignore unsupported controller name.
			continue
		}
		ctrl, err := ext.UnmarshalConfig(rawJSON)
		if err != nil {
			return fmt.Errorf("cannot unmarshal pairing controller %q config: %w", name, err)
		}
		c.PairingControllers[name] = ctrl
	}
	return nil
}

func (c *PairingControllerConfigs) CreateControllers(p PairingAccessor) ([]Controller, error) {
	controllers := []Controller{}
	for name, config := range c.PairingControllers {
		ext, ok := controllerExtensions[name]
		if !ok {
			// Ignore unsupported controller name.
			continue
		}
		controller, err := ext.NewController(p, config)
		if err != nil {
			return nil, err
		}
		controllers = append(controllers, controller)
	}
	return controllers, nil
}
