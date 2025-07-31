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
	"time"

	"github.com/canonical/pebble/internals/logger"
)

const PowerOnControllerName = "poweron"

type PowerOnControllerExtension struct{}

// UnmarshalConfig unmarshals the config JSON into a concrete backing type.
func (e *PowerOnControllerExtension) UnmarshalConfig(r json.RawMessage) (ControllerConfig, error) {
	cfg := &PowerOnControllerConfig{}
	err := json.Unmarshal(r, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (e *PowerOnControllerExtension) NewController(p PairingAccessor, c ControllerConfig) (Controller, error) {
	config := c.(*PowerOnControllerConfig)
	controller := &PowerOnController{
		config:        config,
		pairingAccess: p,
	}
	return controller, nil
}

type PowerOnControllerConfig struct {
	// The maximum duration the pairing window is open following
	// a device power-on.
	Duration StringDuration `json: "duration"`
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
	config        *PowerOnControllerConfig
	pairingAccess PairingAccessor
	timer         *time.Timer
}

func NewPowerOnController(config *PowerOnControllerConfig, pairingAccess PairingAccessor) *PowerOnController {
	if config == nil {
		config = &PowerOnControllerConfig{}
	}
	if config.Duration.Duration() <= time.Duration(0) {
		config.Duration = StringDuration(pairingWindowDuratonDefault)
	}

	p := &PowerOnController{
		config:        config,
		pairingAccess: pairingAccess,
	}
	// Time since Linux kernel boot (true even inside containers).
	boottime := GetLinuxKernelRuntime()

	if time.Duration(boottime) < powerOnBootExpiry {
		err := p.pairingAccess.EnablePairing()
		if err != nil {
			logger.Noticef("Cannot enable power-on pairing: %s", err)
			return nil
		}

		// Schedule disable of pairing window.
		p.timer = time.AfterFunc(p.config.Duration.Duration(), p.disablePairing)
	}
	return nil
}

func (p *PowerOnController) disablePairing() {
	err := p.pairingAccess.DisablePairing()
	if err != nil {
		logger.Noticef("Cannot disable power-on pairing: %s", err)
	}
}

func (p *PowerOnController) PairingDisabled() {
	// Cancel sceduled pairing window disable, since the pairing
	// manager has already done that.
	p.timer.Stop()
}
