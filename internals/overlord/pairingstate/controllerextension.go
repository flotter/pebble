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
	"fmt"
	"gopkg.in/yaml.v3"
)

var (
	// Registered pairing controller extensions.
	controllerExtensions = map[string]ControllerExtension{}
)

// PairingAccessor provides access to pairing manager functions for controlling
// the pairing window.
type PairingAccessor interface {
	// EnablePairing requests the pairing manager to enable the pairing
	// window. If the pairing window cannot be enabled, or if it is already
	// enabled, an error will be returned.
	EnablePairing() error

	// DisablePairing requests the pairing manager to disable the pairing
	// window. If the pairing window is already disabled, an error will
	// be returned.
	DisablePairing() error
}

type ControllerExtension interface {
	// ParseSection returns a newly allocated concrete type containing the
	// unmarshalled config content.
	ParseConfig(data yaml.Node) (ControllerConfig, error)

	// CombineSections returns a newly allocated concrete type containing the
	// result of combining the supplied configs in order.
	CombineConfigs(configs ...ControllerConfig) (ControllerConfig, error)

	// NewController creates a new pairing manager controller without a
	// configuraton. NewController is not allowed to use the
	// PairingAccessor until a configuration is supplied (Ensured).
	NewController(accessor PairingAccessor) Controller
}

// ControllerConfig represents a pairing controller specific concrete
// configuration type. The interface methods provides compatability with
// the plan extension system
type ControllerConfig interface {
	// Equal returns true if the supplied configuration is identical.
	Equal(config ControllerConfig) bool

	// IsZero returns true of the type is empty (should not be marshalled)
	IsZero() bool

	// Validate returns an error if validation of the content fails.
	Validate() error
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
