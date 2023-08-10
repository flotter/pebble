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

package daemon

import (
	"encoding/json"
	"mime"
	"net/http"

	"github.com/canonical/pebble/internals/overlord/restart"
)

type deviceResult struct {
	Error *errorResult `json:"error,omitempty"`
}

func v1PostDevice(c *Command, req *http.Request, _ *userState) Response {
	contentType := req.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return statusBadRequest("invalid Content-Type %q", contentType)
	}

	switch mediaType {
	case "application/json":
		var payload struct {
			Action string `json:"action"`
		}
		decoder := json.NewDecoder(req.Body)
		if err := decoder.Decode(&payload); err != nil {
			return statusBadRequest("cannot decode request body: %v", err)
		}
		switch payload.Action {
		case "reboot":
			return requestReboot(c.d)
		default:
			return statusBadRequest("invalid action %q", payload.Action)
		}
	default:
		return statusBadRequest("invalid media type %q", mediaType)
	}
}

// requestReboot asks the daemon to immediately shut down the system
// and issue a reboot.
func requestReboot(d *Daemon) Response {
	d.HandleRestart(restart.RestartSystem)
	result := deviceResult{}
	return SyncResponse(result)
}
