// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package fwstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"
)

// fwSetupAndState loads custom data element "firmware-setup" to determine firmware name
// and revision and loads the current firmware state. If the state cannot be found at this
// point the requested firmware is not available.
func fwSetupAndState(t *state.Task) (*FwSetup, *FwState, error) {
	var fwSetup FwSetup
	err := t.Get("firmware-setup", &snapsup)
	if err != nil {
		return nil, nil, fmt.Errorf("required data firmware-setup not available %w", err)
	}
	var fwState FwState
	err = Get(t.State(), fwSetup.Name(), &fwState)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to locate firmware state for %s %w", fwSetup.Name(), err)
	}
	return fwSetup, &fwState, nil
}

/* State Locking

   do* / undo* handlers should usually lock the state just once with:

	st.Lock()
	defer st.Unlock()

   For tasks doing slow operations (long i/o, networking operations) it's OK
   to unlock the state temporarily:

        st.Unlock()
        err := slowIOOp()
        st.Lock()
        if err != nil {
           ...
        }

    but if a task Get and then Set the SnapState of a snap it must avoid
    releasing the state lock in between, other tasks might have
    reasons to update the SnapState independently:

        // DO NOT DO THIS!:
        snapst := ...
        snapst.Attr = ...
        st.Unlock()
        ...
        st.Lock()
        Set(st, snapName, snapst)

    if a task really needs to mix mutating a SnapState and releasing the state
    lock it should be serialized at the task runner level, see
    SnapManger.blockedTask and TaskRunner.SetBlocked

*/

// doBootloaderConfigUpdate selects which firmware slot should boot
func (m *FwManager) doBootloaderConfigUpdate(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	fwSetup, fwState, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// Convert the revision to a slot.
	var slot Bootslot
	for s, si := range fwState.Installs {
		if si.revision == fwSetup.SideInfo.revision {
			slot = s
		}
	}
	if slot == "" {
		return fmt.Errorf("unable to locate firmware revision %s", fwSetup.SideInfo.revision.Name())
	}
	if slot == prevSlot {
		return fmt.Errorf("firmware revision %s already set for boot", fwSetup.SideInfo.revision.Name())
	}

	// Record previous boot slot to allow for an undo
	prevSlot, err := m.backend.GetBootSlot()
	if err != nil {
		return err
	}
	t.Set("fwstate-prev-bootloader-cfg", prevSlot)

	// Update the platform bootloader configuration
	err = m.backend.SetBootSlot(slot)
	if err != nil {
		return err
	}

	return nil
}

// undoBootloaderConfigUpdate reverts the bootloader configuration apon failure
func (m *FwManager) undoBootloaderConfigUpdate(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	var prevSlot Bootslot
	if err := t.Get("fwstate-prev-bootloader-cfg", &prevSlot); err != nil && !errors.Is(err, state.ErrNoState) {
                return err
        }
	// If the boot slot has not been set, there is nothing to be undone. This means
	// the do task failed before a change was applied.
	if prevSlot == "" {
		return nil
	}

	// Update the platform bootloader configuration
	err = m.backend.SetBootSlot(prevSlot)
	if err != nil {
		return err
	}

	return nil
}

// doFwStateSetBoot changes the firmware state to reflect a different boot config
func (m *FwManagder) doFwStateSetBoot(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	fwSetup, fwState, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// Find and clear the active boot flag, but record which
	// firmware was cleared so we can undo this if we need to.
	all, err := All(st)
        if err != nil {
                return err
        }
	for name, st := range all {
		if st.Boot == true {
			t.Set("fwstate-prev-boot-fw", name)
			st.Boot = false
		}
	}

	// Update the firmware of interest while recoring any
	// change we make to Current (the selected revision).
	var slot Bootslot
	for s, si := range fwState.Installs {
		if si.revision == fwSetup.SideInfo.revision {
			slot = s
		}
	}
	if slot == "" {
		return fmt.Errorf("unable to locate firmware revision %s", fwSetup.SideInfo.revision.Name())
	}
	t.Set("fwstate-prev-boot-current", fwState.Current)
	fwState.Current = slot
	fwState.Boot = true
}

// undoFwStateSetBoot restores the firmware state to an earlier boot config
func (m *FwManagder) undoFwStateSetBoot(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	fwSetup, fwState, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	var prevSlot Bootslot
	if err := t.Get("fwstate-prev-boot-current", &prevSlot); err != nil && !errors.Is(err, state.ErrNoState) {
                return err
        }
	// Only if the current slot was recorded do we have a change to undo. We are undoing in reverse order
	// so if nothing happens here, we need to check the previous change (in the same task) next.
	if prevSlot != "" {
		var slot Bootslot
		for s, si := range fwState.Installs {
			if si.revision == fwSetup.SideInfo.revision {
				slot = s
			}
		}
		if slot == "" {
			return fmt.Errorf("unable to locate firmware revision %s", fwSetup.SideInfo.revision.Name())
		}
		fwState.Current = prevSlot
		fwState.Boot = false
	}

	var prevFwName string
	if err := t.Get("fwstate-prev-boot-fw", &prevFwName); err != nil && !errors.Is(err, state.ErrNoState) {
                return err
        }
	// If the firmware name has not been set, there is nothing to be undone. This means
	// the do task failed before this change was applied.
	if prevFwName != "" {
		// Restore previously cleared boot flag
		var prevFwState FwState
		err = Get(st, prevFwName, &prevFwState)
	        if err != nil {
	                return err
	        }
		prevFwState.Boot = true
		err = Set(st, prevFwName, prevFwState)
	        if err != nil {
	                return err
	        }
	}
}
