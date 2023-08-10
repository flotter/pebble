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

package cli

import (
	"github.com/canonical/go-flags"
	"github.com/canonical/pebble/client"
)

const cmdRebootSummary = "Reboot device"
const cmdRebootDescription = "The reboot command reboots the device."

type cmdReboot struct {
	clientMixin
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "reboot",
		Summary:     cmdRebootSummary,
		Description: cmdRebootDescription,
		Builder: func() flags.Commander { return &cmdReboot{} },
	})
}

func (cmd *cmdReboot) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return cmd.client.Reboot(&client.DeviceOptions{})
}
