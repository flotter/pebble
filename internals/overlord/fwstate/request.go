// Copyright (c) 2023 Canonical Ltd
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

package fwstate

import (
	"fmt"

	"github.com/canonical/pebble/internals/overlord/state"
)

type RefreshOptions struct {
	Upload bool  `json:"upload"`// False for store based refresh
	Target string `json:"target"`
}

// Update the non-running slot with the latest firmware
// and configure the system to refresh to the new
// firmware following a user reboot.
func Refresh(s *state.State, opts *RefreshOptions) (*state.TaskSet, error) {
	var tasks []*state.Task

	// We only support local upload right now
	if opts.Upload == false {
		return nil, fmt.Errorf("store refresh not implemented")
	}

	task := s.NewTask("firmware-refresh-prepare", "Validate and prepare for refresh")
	tasks = append(tasks, task)

	task = s.NewTask("firmware-refresh-upload", "Receiving firmware payload")
	tasks = append(tasks, task)

	task = s.NewTask("firmware-refresh-complete", "Verify and complete")
	tasks = append(tasks, task)

	return state.NewTaskSet(tasks...), nil
}
