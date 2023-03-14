// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package firmware

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Info provides information about firmware.
type Info struct {
	Version             string
	Base                string
	OriginalName        string
	OriginalTitle       string
	OriginalSummary     string
	OriginalDescription string

	// The information in all the remaining fields is not sourced from
	// the firmware payload YAML itself.

	SideInfo
}

// SideInfo holds firmware metadata for which the store is the
// canonical source.
//
// It can be marshalled and will be stored in the system state for
// each installed firmware so it needs to be evolved carefully.
type SideInfo struct {
	Revision Revision `yaml:"revision" json:"revision"`

	ApprovedName        string `yaml:"name,omitempty" json:"name,omitempty"`
	ApprovedTitle       string `yaml:"title,omitempty" json:"title,omitempty"`
	ApprovedSummary     string `yaml:"summary,omitempty" json:"summary,omitempty"`
	ApprovedDescription string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Revision returns the revision of the firmware.
func (info *Info) Revision() Revision {
	return info.Revision
}

// Base returns the base version of the firmware.
func (info *Info) Base() string {
	return info.Base
}

// Name returns the global blessed name of the firmware.
func (info *Info) Name() string {
	if info.ApprovedName != "" {
		return info.ApprovedName
	}
	return info.OriginalName
}

// Summary returns the global blessed summary of the firmware.
func (info *Info) Summary() string {
	if info.ApprovedSummary != "" {
		return info.ApprovedSummary
	}
	return info.OriginalSummary
}

// Description returns the global blessed description of the firmware.
func (info *Info) Description() string {
	if info.ApprovedDescription != "" {
		return info.ApprovedDescription
	}
	return info.OriginalDescription
}

func infoFromFirmwareYamlWithSideInfo(meta []byte, si *SideInfo, strk *scopedTracker) (*Info, error) {
        info, err := infoFromSnapYaml(meta, strk)
        if err != nil {
                return nil, err
        }

        if si != nil {
                info.SideInfo = *si
        }

        return info, nil
}

// fwSlotsMountBase point to the base directory where the available
// firmware slots will be mounted
const fwSlotsMountBase = "/kernos/slots"

// ReadFwSlotInfo reads the firmware slot information
func ReadFwSlotInfo(slot string) (*Info, error) {
        fwYamlFile := filepath.Join(fwSlotsMountBase, slot, "firmware.yaml")
        meta, err := ioutil.ReadFile(fwYamlFile)
        if os.IsNotExist(err) {
		return nil, fmt.Errorf("firmware slot %s not found: %w", slot, err)
        }
        if err != nil {
                return nil, err
        }

        info, err := infoFromFwYaml(meta)
        if err != nil {
                return nil, fmt.Errorf("invalid metadata for slot %s: %w", slot, err)
        }

	return info, nil
}
