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

package pairingstate_test

import (
	"encoding/json"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
)

func (ps *pairingSuite) TestManagerFromJSON(c *C) {
	pairingstate.RegisterPairingController(pairingstate.PowerOnControllerName, &pairingstate.PowerOnControllerExtension{})
	defer pairingstate.UnregisterPairingController(pairingstate.PowerOnControllerName)

	configJSON := []byte(`
{
  "pairing-mode": "single",
  "pairing-controllers": {
    "poweron": {
      "duration": "60s"
    }
  }
}
`[1:])

	config := &pairingstate.Config{}
	err := json.Unmarshal(configJSON, &config)
	c.Assert(err, IsNil)

	manager, err := pairingstate.NewPairingManager(nil, config)
	c.Assert(err, IsNil)

	err = manager.Ensure()
	c.Assert(err, IsNil)
}

func (ps *pairingSuite) TestManagerFromConfig(c *C) {
	pairingstate.RegisterPairingController(pairingstate.PowerOnControllerName, &pairingstate.PowerOnControllerExtension{})
	defer pairingstate.UnregisterPairingController(pairingstate.PowerOnControllerName)

	config := &pairingstate.Config{
		PairingControllerConfigs: pairingstate.PairingControllerConfigs{
			PairingControllers: map[string]pairingstate.ControllerConfig{
				pairingstate.PowerOnControllerName: &pairingstate.PowerOnControllerConfig{
					Duration: pairingstate.StringDuration(30 * time.Second),
				},
			},
		},
	}

	manager, err := pairingstate.NewPairingManager(nil, config)
	c.Assert(err, IsNil)

	err = manager.Ensure()
	c.Assert(err, IsNil)
}
