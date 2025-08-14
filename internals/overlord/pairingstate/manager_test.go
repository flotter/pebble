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
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/plan"
)

func (ps *pairingSuite) TestBasic(c *C) {
	layersDir := c.MkDir()
	mgr, err := planstate.NewManager(layersDir)
	c.Assert(err, IsNil)

	// Register the Pairing controller extension.
	pairingstate.RegisterPairingController(pairingstate.PowerOnControllerName, &pairingstate.PowerOnControllerExtension{})
	defer pairingstate.UnregisterPairingController(pairingstate.PowerOnControllerName)

	// Register the Plan extension.
	ext, err := pairingstate.NewSectionExtension()
	c.Assert(err, IsNil)
	plan.RegisterSectionExtension(pairingstate.PairingField, ext)
	defer plan.UnregisterSectionExtension(pairingstate.PairingField)

//	// Write test layer
//	layer := `
//		summary: layer-1
//		description: desc of layer-1
//		pairing:
//			override: merge
//			mode: single
//			controller:
//				type: power-on`
//	writeLayer(c, layersDir, string(reindent(layer)), 0)

	// Load the plan layers
	err = mgr.Load()
	c.Assert(err, IsNil)

	// Verify the combined marshalled plan is correct.
	c.Assert(planYAML(c, mgr), Equals, string(reindent(`
		pairing:`)))
}
