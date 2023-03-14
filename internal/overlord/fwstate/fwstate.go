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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of firmwares.
package fwstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// runningFirmware returns the running firmware state
func runningFirmware(st *state.State) (*FwState, error) {
	all, err := All(st)
        if err != nil {
                return nil, err
        }
	for _, st := range all {
		if st.Running == true {
			return st, nil
		}
	}
	return nil, fmt.Errorf("no running firmware state found")
}

// firmwareBySlot returns firmware name and revision associated with a boot slot.
func firmwareBySlot(st *state.State, bootSlot Bootslot) (string, *firmware.Revision, error) {
	all, err := All(st)
        if err != nil {
                return "", nil, err
        }
	var fwState Fwstate
	for name, st := range all {
		for slot, si := range st.Installs {
			if slot ==  bootSlot {
				return name, &si.Revision, nil
			}
		}
	}
	return "", nil, nil
}

// Revert provides a helper to revert between the slots A/B.
func Revert(st *state.State) (*state.TaskSet, error) {
	fwState := runningFirmware(st)
        if err != nil {
                return nil, err
        }
	currentSlot := fwState.Current
	// Flip to the other slot of A/B
	revertSlot := KernosA
	if currentSlot == KernosA {
		revertSlot = KernosB
	}
	name, rev, err := firmwareBySlot(st, revertSlot)
        if err != nil {
                return nil, err
        }
	if name == "" {
		return nil, fmt.Errorf("no other installed firmware to revert to", name, rev.String())
	}
	return RevertToRevision(st, name, rev)
}

// RevertToRevision performs validation of a revert request and returns the tasks
// required to apply a revert.
func RevertToRevision(st *state.State, name string, rev firmware.Revision) (*state.TaskSet, error) {
        var fwState FwState
        err := Get(st, name, &fwState)
        if err != nil && !errors.Is(err, state.ErrNoState) {
                return nil, err
        }
	// Does the revision exist?
	found := false
	for _, si := range fwState.Installs {
		if si.Revision == rev {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("firmware %s revision %s not found", name, rev.String())
	}
	return doRevert(st, FwSetup{SideInfo: &firmware.SideInfo{ApprovedName: name, Revision: rev}})
}

// doRevert take a firmware name and revision from a pre-validated FwSetup structure
// and return the tasks required to implement the change.
func doRevert(st *state.State, fwSetup *FwSetup) (*state.TaskSet, error) {
	// Create task to modify the bootloader configuration
	bootloaderCfgChange := st.NewTask("bootloader-cfg-change", fmt.Sprintf("Configure bootloader to next boot firmware %s revision %s", fwSetup.Name(), fwSetup.Revision()))
        bootloaderCfgChange.Set("firmware-setup", fwSetup)
	// Create task to update the firmware state with the requested change
	bootStateChange := st.NewTask("boot-state-change", fmt.Sprintf("Update firmware state to reflect latest config changes to firmware %s revision %s", fwSetup.Name(), fwSetup.Revision()))
        bootStateChange.Set("firmware-setup", fwSetup)
	// Ensure state update only after bootloader change success
	bootStateChange.WaitFor(bootloaderCfgChange)

	ts := state.NewTaskSet(bootloaderCfgChange, bootStateChange)
	return ts, nil
}

// Get retrieves the FwState of the given firmware.
func Get(st *state.State, name string, fwState *FwState) error {
	if fwState == nil {
		return fmt.Errorf("internal error: fwState is nil")
	}
	// FwState is (un-)marshalled from/to JSON, fields having omitempty
	// tag will not appear in the output (if empty) and subsequently will
	// not be unmarshalled to (or cleared); if the caller reuses the same
	// struct though subsequent calls, it is possible that they end up with
	// garbage inside, clear the destination struct so that we always
	// unmarshal to a clean state
	*fwState = FwState{}

	var firmwares map[string]*json.RawMessage
	err := st.Get("firmwares", &firmwares)
	if err != nil {
		return err
	}
	raw, ok := firmwares[name]
	if !ok {
		return state.ErrNoState
	}
	err = json.Unmarshal([]byte(*raw), fwState)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap state: %v", err)
	}
	return nil
}

// All retrieves return a map from name to FwState for all current firmwares in the system state.
func All(st *state.State) (map[string]*FwState, error) {
	// XXX: result is a map because sideloaded snaps carry no name
	// atm in their sideinfos
	var stateMap map[string]*FwState
	if err := st.Get("firmwares", &stateMap); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	curStates := make(map[string]*FwState, len(stateMap))
	for instanceName, fwState := range stateMap {
		curStates[instanceName] = fwState
	}
	return curStates, nil
}

// Set sets the SnapState of the given snap, overwriting any earlier state.
// Note that a SnapState with an empty Sequence will be treated as if fwState was
// nil and name will be deleted from the state.
func Set(st *state.State, name string, fwState *SnapState) {
	var firmwares map[string]*json.RawMessage
	err := st.Get("firmwares", &firmwares)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		panic("internal error: cannot unmarshal firmware states: " + err.Error())
	}
	if firmwares == nil {
		firmwares = make(map[string]*json.RawMessage)
	}
	if fwState == nil || (len(fwState.Installs) == 0) {
		delete(firmwares, name)
	} else {
		data, err := json.Marshal(fwState)
		if err != nil {
			panic("internal error: cannot marshal firmware state: " + err.Error())
		}
		raw := json.RawMessage(data)
		firmwares[name] = &raw
	}
	st.Set("firmwares", firmwares)
}

