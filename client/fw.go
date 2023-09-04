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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"os"
	"strings"
)

var _ os.FileInfo = (*FileInfo)(nil)

// FwFile allows a local firmware to be supplied instead
// of contacting a store for the refresh.
type FwFile struct {
	Source      io.Reader
	Size        int64
	UploadError chan error
}

func (fw FwFile) Validate() error {
	if fw.Source == nil || fw.Size <= 0 || fw.UploadError == nil {
		return fmt.Errorf("invalid firmware file description")
	}
	return nil
}

// RefreshOptions contains all refresh related options
type RefreshOptions struct {
	Firmware *FwFile // Optional
}

type refreshPayload struct {
	// refresh        : store based refresh request
	// refresh-local  : refresh includes an upload step
	Action string `json:"action"`
}

type uploadPayload struct {
	// upload         : upload a file to the device
	Action string `json:"action"`
	Size   int64  `json:"size"`
}

// Refresh requests the device to perform a firmware refresh
func (client *Client) Refresh(opts *RefreshOptions) (changeID string, err error) {
	if opts.Firmware == nil {
		// TODO: Implement store support
		return "", fmt.Errorf("unable to perform store refresh, not implemented yet")
	}

	if err := opts.Firmware.Validate(); err != nil {
		return "", err
	}

	payload := refreshPayload{
		Action: "refresh-upload",
	}
	data, err := json.Marshal(&payload)
	if err != nil {
		return "", fmt.Errorf("cannot marshal refresh payload: %w", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	_, changeID, err = client.doAsyncFull("POST", "/v1/firmware", nil, headers, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}

	// Background file upload
	go func() {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)

		// Encode metadata part of the header
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":        {"application/json"},
			"Content-Disposition": {`form-data; name="upload"`},
		})
		if err != nil {
			opts.Firmware.UploadError <- fmt.Errorf("cannot encode metadata in request payload: %w", err)
			return
		}

		payload := uploadPayload{
			Action: "upload",
			Size:   opts.Firmware.Size,
		}
		if err = json.NewEncoder(part).Encode(&payload); err != nil {
			opts.Firmware.UploadError <- err
			return
		}

		// Encode file part of the header
		_, err = mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":        {"application/octet-stream"},
			"Content-Disposition": {`form-data; name="file"`},
		})
		if err != nil {
			opts.Firmware.UploadError <- fmt.Errorf("cannot encode file in request payload: %w", err)
			return
		}

		header := b.String()

		// Encode multipart footer
		b.Reset()
		mw.Close()
		footer := b.String()

		var result fileResult
		body := io.MultiReader(strings.NewReader(header), opts.Firmware.Source, strings.NewReader(footer))
		headers := map[string]string{
			"Content-Type": mw.FormDataContentType(),
		}
		if _, err := client.doSync("POST", "/v1/firmware", nil, headers, body, &result); err != nil {
			opts.Firmware.UploadError <- err
			return
		}

		if result.Error != nil {
			opts.Firmware.UploadError <- &Error{
				Kind:    result.Error.Kind,
				Value:   result.Error.Value,
				Message: result.Error.Message,
			}
			return
		}
	}()

	return changeID, nil
}
