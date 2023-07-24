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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// RebootOptions hold options for the reboot command
type DeviceOptions struct {}

type devicePayload struct {
	Action string `json:"action"`
}

type deviceResult struct {
	Error *Error `json:"error,omitempty"`
}

// Reboot asks the device to reboot
func (client *Client) Reboot(opts *DeviceOptions) error {
	payload := &devicePayload{
		Action: "reboot",
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return fmt.Errorf("cannot encode JSON payload: %w", err)
	}

	var result deviceResult
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if _, err := client.doSync("POST", "/v1/device", nil, headers, &body, &result); err != nil {
		return err
	}

	if result.Error != nil {
		return result.Error
	}
	return nil
}
