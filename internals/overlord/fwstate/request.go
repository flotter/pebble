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
	"github.com/canonical/pebble/internals/overlord/state"
)

// Update the non-running slot with the latest firmware
// and configure the system to refresh to the new
// firmware following a user reboot.
func Refresh(s *state.State) (*state.TaskSet, error) {
	var tasks []*state.Task

	task := s.NewTask("firmware-upload", "Receiving firmware payload")

	tasks = append(tasks, task)

	return state.NewTaskSet(tasks...), nil
}
