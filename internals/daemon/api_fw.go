// Copyright (c) 2021 Canonical Ltd
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
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/canonical/pebble/internals/overlord/fwstate"
)

func absolutePathError(path string) error {
	return fmt.Errorf("paths must be relative to firmware slot, got %q", path)
}

func v1PostFw(c *Command, req *http.Request, _ *UserState) Response {
	contentType := req.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return statusBadRequest("invalid Content-Type %q", contentType)
	}

	switch mediaType {
	case "application/json":
                var payload struct {
		        // refresh        : store based refresh request
			// refresh-local  : refresh includes an upload step
			Action string `json:"action"`
                }
                decoder := json.NewDecoder(req.Body)
                if err := decoder.Decode(&payload); err != nil {
                        return statusBadRequest("cannot decode request body: %v", err)
                }
                switch payload.Action {
		case "refresh-local":
			return localRefreshRequest(c)
		case "refresh":
			return statusBadRequest("unsupported store refresh")
		default:
			return statusBadRequest("unsupported action %q", payload.Action)
		}
	case "multipart/form-data":
		boundary := params["boundary"]
		if len(boundary) < minBoundaryLength {
			return statusBadRequest("invalid boundary %q", boundary)
		}
		return uploadRequest(c, req.Body, boundary)
	default:
		return statusBadRequest("invalid media type %q", mediaType)
	}
}

// localRefreshRequest starts a taskset responsible for starting the
// firmware refresh process. Once the tasks will wait for the firmware
// file supplied through a second POST request.
func localRefreshRequest(c *Command) Response {

	fwMgr := c.Daemon().Overlord().FirmwareManager()
	runningSlot := fwMgr.RunningSlot()

	// Let's ask the Firmware Manager to give us the
	// default install slot
	installSlot, err := fwstate.GetInstallSlot(runningSlot)
	if err != nil {
		return statusBadRequest("unable to refresh: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	opts := &fwstate.RefreshOptions{
                Upload: true,
                Target: installSlot,
        }

	taskSet, err := fwstate.Refresh(st, opts)
	if err != nil {
		return statusBadRequest("unable to refresh: %v", err)
	}

	change := st.NewChange("refresh", "Firmware refresh")
	change.AddAll(taskSet)
	change.Set("firmware-refresh", opts)

	stateEnsureBefore(st, 0)

	return AsyncResponse(nil, change.ID())
}

func uploadRequest(c *Command, body io.Reader, boundary string) Response {

	mr := multipart.NewReader(body, boundary)
	part, err := mr.NextPart()
	if err != nil {
		return statusBadRequest("cannot read request metadata: %v", err)
	}
	if part.FormName() != "upload" {
		return statusBadRequest(`metadata field name must be "upload", got %q`, part.FormName())
	}

	// Decode metadata about files to write.
	var payload struct {
	        Size int64  `json:"size"`
		Id   string `json:"id"` // Change ID
	}

	decoder := json.NewDecoder(part)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request metadata: %v", err)
	}
	if payload.Size <= 0 {
		return statusBadRequest("invalid file size %q bytes", payload.Size)
	}
	if payload.Id == "" {
		return statusBadRequest("invalid file upload change id %q", payload.Id)
	}

	// Receive the file
	part, err = mr.NextPart()
	if err != nil {
		return statusBadRequest("cannot read file part: %v", err)
	}
	if part.FormName() != "file" {
		return statusBadRequest(`field name must be "file", got %q`, part.FormName())
	}

	done := make(chan error)
	defer close(done)

	fwMgr := c.Daemon().Overlord().FirmwareManager()
	fwMgr.SetUploadRequest(payload.Id, &fwstate.UploadRequest{
		Size: payload.Size,
		Source: part,
		Done: done,
	})

	// Wait until the task indicates its done
	err = <-done

	part.Close()

	return SyncResponse(err)
}
