// Copyright (c) 2022 Canonical Ltd
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
	"os"
	"path/filepath"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdRefreshSummary = "Update device firmware"
const cmdRefreshDescription = `
Update device with supplied firmware. Restart required to take effect.
`

type cmdRefresh struct {
	clientMixin

	Slot    string   `long:"slot"`
	Positional struct {
		LocalPath  string `positional-arg-name:"<local-path>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "refresh",
		Summary:     cmdRefreshSummary,
		Description: cmdRefreshDescription,
		ArgsHelp: map[string]string{
			"--slot":   "Valid slots: a, b, f",
		},
		Builder: func() flags.Commander { return &cmdRefresh{} },
	})
}
func (cmd *cmdRefresh) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	f, err := os.Open(cmd.Positional.LocalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	return cmd.client.Refresh(&client.RefreshOptions{
		Source:		f,
		Path:		filepath.Base(cmd.Positional.LocalPath),
		Size:		st.Size(),
		Slot:		cmd.Slot,
	})
}
