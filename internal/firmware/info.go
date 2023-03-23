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
	Version		string
	Name		string
	Summary		string

	// The information in the remaining fields is not sourced from
	// the firmware payload JSON itself.
	AssertedInfo

}

// AssertedInfo holds firmware metadata for which the store is the
// canonical source.
//
// It can be marshalled and will be stored in the system state for
// each installed firmware so it needs to be evolved carefully.
type AssertedInfo struct {
       Revision		Revision	`json:"revision"`
       ApprovedName	string		`json:"name,omitempty"`
       ApprovedSummary	string		`json:"summary,omitempty"`
}

// Revision returns the revision of the firmware.
func (info *Info) Revision() Revision {
       return info.Revision
}

// Name returns the blessed name of the firmware.
func (info *Info) Name() string {
	if info.ApprovedName != "" {
		return info.ApprovedName
	}
	return info.Name
}

// Summary returns the blessed summary of the firmware.
func (info *Info) Summary() string {
	if info.ApprovedSummary != "" {
		return info.ApprovedSummary
	}
	return info.Summary
}

// GetInfo returns a populated Info structure describing the running firmware.
func GetInfo() (*Info, error) {
	// Information from the JSON will eventually be suplimented with
	// additional attributes coming from assertions.
	return infoFromRunning()
}
