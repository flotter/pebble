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


// PushOptions contains the options for a call to Push.
type RefreshOptions struct {
	// Source is the source of data to write (required).
	Source io.Reader

	// Path indicates the absolute path of the file in the destination
	// machine (required).
	Path string
	Size int64
	Slot string
}

type filePayload struct {
	Action string           `json:"action"`
	Slot   string		`json:"slot"`
	File  fileInfo		`json:"file"`
}

type fileInfo struct {
	Path        string `json:"path"`
	Size        int64 `json:"size"`
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// Push writes content to a path on the remote system.
func (client *Client) Refresh(opts *RefreshOptions) error {
	// Buffer for multipart header/footer
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)

	// Encode metadata part of the header
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {"application/json"},
		"Content-Disposition": {`form-data; name="request"`},
	})
	if err != nil {
		return fmt.Errorf("cannot encode metadata in request payload: %w", err)
	}

	payload := filePayload{
		Action: "refresh",
		Slot: opts.Slot,
		File: fileInfo {
			Path: opts.Path,
			Size: opts.Size,
		},
	}
	if err = json.NewEncoder(part).Encode(&payload); err != nil {
		return err
	}

	// Encode file part of the header
	escapedPath := escapeQuotes(opts.Path)
	_, err = mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {"application/octet-stream"},
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapedPath)},
	})
	if err != nil {
		return fmt.Errorf("cannot encode file in request payload: %w", err)
	}

	header := b.String()

	// Encode multipart footer
	b.Reset()
	mw.Close()
	footer := b.String()

	var result fileResult
	body := io.MultiReader(strings.NewReader(header), opts.Source, strings.NewReader(footer))
	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}
	if _, err := client.doSync("POST", "/v1/device/firmware", nil, headers, body, &result); err != nil {
		return err
	}

	if result.Error != nil {
		return &Error{
			Kind:    result.Error.Kind,
			Value:   result.Error.Value,
			Message: result.Error.Message,
		}
	}

	return nil
}
