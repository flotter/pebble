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
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/plan"
)

const PowerOnControllerName = "power-on"

type PowerOnControllerExtension struct{}

// NewController returns a new power on controller instance.
func (e *PowerOnControllerExtension) NewController(pairingAccess PairingAccessor) Controller {
	return NewPowerOnController(pairingAccess)
}

// EmptyConfig returns a new controller specific empty configuration.
func (e *PowerOnControllerExtension) ParseConfig(data yaml.Node) (ControllerConfig, error) {
	controllerConfig := &PowerOnControllerConfig{}
	// The following issue prevents us from using the yaml.Node decoder
	// with KnownFields = true behaviour. Once one of the proposals get
	// merged, we can remove the intermediate Marshall step.
	// https://github.com/go-yaml/yaml/issues/460
	if len(data.Content) != 0 {
		yml, err := yaml.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot marshal controller config: %w", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(yml))
		dec.KnownFields(true)
		if err = dec.Decode(controllerConfig); err != nil {
			return nil, &plan.FormatError{
				Message: fmt.Sprintf("cannot parse the controller config: %v", err),
			}
		}
	}

	return controllerConfig, nil
}

func (e *PowerOnControllerExtension) CombineConfigs(configs ...ControllerConfig) (ControllerConfig, error) {
	controllerConfig := &PowerOnControllerConfig{}
	for _, c := range configs {
		configLayer := c.(*PowerOnControllerConfig)
		controllerConfig.merge(configLayer)
	}
	// Apply duration default if zero or unset.
	if !controllerConfig.Duration.IsSet {
		controllerConfig.Duration.Value = pairingWindowDuratonDefault
		controllerConfig.Duration.IsSet = true
	}
	return controllerConfig, nil
}

type PowerOnControllerConfig struct {
	// The maximum duration the pairing window is open following
	// a device power-on.
	Duration OptionalDuration `json: "duration"`
}

func (c *PowerOnControllerConfig) Equal(controllerConfig ControllerConfig) bool {
	config := controllerConfig.(*PowerOnControllerConfig)
	if c.Duration.Value != config.Duration.Value {
		return false
	}
	return true
}

func (c *PowerOnControllerConfig) merge(other *PowerOnControllerConfig) {
	// Update duration if non-zero.
	if !other.Duration.IsZero() {
		c.Duration.Value = other.Duration.Value
	}
}

func (c *PowerOnControllerConfig) IsZero() bool {
	// We always show the duration, or default duration if zero.
	return false
}

func (c *PowerOnControllerConfig) Validate() error {
	if c.Duration.IsNegative() {
		return fmt.Errorf("negative duration %q not supported", c.Duration.Value)
	}
	return nil
}

const (
	// powerOnBootExpiry defines a duration since the time the Linux kernel
	// booted during which this specific pairing controller will request the
	// pairing window to be enabled during the controller startup. If the
	// controller startup falls after this expiry period, the controller will
	// intepret that as a process restart, not a host power-on event, and
	// leave the pairing window disabled.
	powerOnBootExpiry = 30 * time.Second

	// pairingWindowDuratonDefault defines the maximum duration the pairing
	// window will stay open, if no duration is supplied with the config.
	pairingWindowDuratonDefault = 30 * time.Second
)

type PowerOnController struct {
	mu            sync.Mutex
	config        *PowerOnControllerConfig
	pairingAccess PairingAccessor

	timer *time.Timer
}

func NewPowerOnController(pairingAccess PairingAccessor) *PowerOnController {
	controller := &PowerOnController{
		pairingAccess: pairingAccess,
	}
	return controller
}

func (p *PowerOnController) Type() string {
	return PowerOnControllerName
}

func (p *PowerOnController) Config() ControllerConfig {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.config
}

func (p *PowerOnController) PairingDisabled(reason DisableReason) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
	}
	logger.Debugf("Power-on controller received pairing disabled: %s", reason.String())
}

func (p *PowerOnController) EnsureConfig(c ControllerConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Shutdown request.
	if c == nil && p.timer != nil {
		p.timer.Stop()
	}

	// This controller does not support reconfiguration once the pairing
	// window was opened (timer is not nil).
	if p.timer != nil {
		return nil
	}

	boottime := osutil.GetLinuxKernelRuntime()
	if time.Duration(boottime) < powerOnBootExpiry {
		p.timer = time.AfterFunc(p.config.Duration.Value, p.disablePairing)
		err := p.pairingAccess.EnablePairing()
		if err != nil {
			p.timer.Stop()
			logger.Noticef("Cannot enable pairing using power-on controller: %s", err)
		}
	}
	return nil
}

func (p *PowerOnController) disablePairing() {
	p.mu.Lock()
	defer p.mu.Unlock()

	err := p.pairingAccess.DisablePairing()
	if err != nil {
		logger.Noticef("Cannot disable pairing using power-on controller: %s", err)
	}
}
