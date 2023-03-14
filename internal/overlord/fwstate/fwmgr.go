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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internal/firmware"
)

// FwManager is responsible for firmware refresh and revert.
type FwManager struct {
	state   *state.State
	backend fwBackend
}

// FwSetup holds details for operations supported by FwManager
type FwSetup struct {
	// Using the firmware name and revision we can always locate
	// the matching firmware state, and retrieve additional
	// information if needed.
	SideInfo     *firmware.SideInfo     `json:"side-info,omitempty"`
}

func (fwSetup *FwSetup) Name() string {
	if fwSetup.SideInfo.ApprovedName == "" {
		panic("fwSetup.SideInfo.ApprovedName not set")
	}
	return fwSetup.SideInfo.ApprovedName
}

func (fwSetup *FwSetup) Revision() firmware.Revision {
	return fwSetup.SideInfo.Revision
}

type Bootslot string
const (
	KernosF Bootslot = "kernos-f"
	KernosA Bootslot = "kernos-a"
	KernosB Bootslot = "kernos-b"
)


// FwState holds the state of installed firmware. Note that multiple
// firmwares can exist on the same device, each firmware requiring its
// own FwState instance.
type FwState struct {
	// A firmware can have one or more revisions installed in slots
	Installs map[Bootslots]*firmware.SideInfo `json:"slots"`

	// Which revision of this firmware should be used
	Current Bootslot `json:"current"`

	// Which firmware is selected for boot.
	Boot bool `json:"boot,omitempty"`

	// Which firmware is actively running now.
	Running bool `json:"running,omitempty"`
}

// LocalRevision returns the "latest" local revision. Local revisions
// start at -1 and are counted down.
func (fwState *FwState) LocalRevision() firmware.Revision {
	var local firmware.Revision
	for _, sideInfo := range fwState.Installs {
		if sideInfo.Revision.Local() && sideInfo.Revision.N < local.N {
			local = sideInfo.Revision
		}
	}
	return local
}

// CurrentSideInfo returns the side info for currently running firmware
func (fwState *FwState) CurrentSideInfo() *firmware.SideInfo {
	if fwState.Current == "" {
		panic("fwState.Current slot is not set")
	}
	install, ok := fwState.Installs[fwState.Current]
	if !ok {
		panic("fwState.Current slot is not installed")
	}
	return install
}

var ErrNoCurrent = errors.New("firmware has no current revision")

// Retrieval functions

const (
	errorOnBroken = 1 << iota
	withAuxStoreInfo
)

var fwSlotReadInfo = firmware.ReadSlotInfo

// CurrentInfo returns the information about the current active revision.
func (fwState *FwState) CurrentInfo() (*snap.Info, error) {
	si := fwState.CurrentSideInfo()
	if si == nil {
		return nil, ErrNoCurrent
	}

	info, err := fwSlotReadInfo(fwState.Current)
	if err != nil {
		return nil, err
	}

	info.SideInfo = *si
	return info
}

// Manager returns a new firmware manager.
func Manager(st *state.State, runner *state.TaskRunner) (*FwManager, error) {
	m := &FwManager{
		state:		st,
		backend:	backend.Backend{}
	}

	// Refresh / Revert related handlers
	runner.AddHandler("bootloader-cfg-change", m.doBootloaderConfigUpdate, m.undoBootloaderConfigUpdate)
	runner.AddHandler("boot-state-change", m.doFwStateSetBoot, m.undoFwStateSetBoot)

	return m, nil
}

// StartUp is a state engine early initialization hook that updates the firmware
// state based on actual slot contents and boot details collected during bootstrap
// before the overlord event loop is started.
func (m *SnapManager) StartUp() error {
	return nil
}

// Implement the State Engine interfaces once the firmware manager is required to
// act on any of these global requests.
//
//func (m *SnapManager) Ensure() error {
//	return nil
//}
//
//func (m *SnapManager) Wait() error {
//	return nil
//}
//
//func (m *SnapManager) Stop() error {
//	return nil
//}
