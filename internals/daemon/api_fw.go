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
	pathpkg "path"
	"path/filepath"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
)

func absolutePathError(path string) error {
	return fmt.Errorf("paths must be relative to firmware slot, got %q", path)
}

func v1PostFw(_ *Command, req *http.Request, _ *UserState) Response {
	contentType := req.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return statusBadRequest("invalid Content-Type %q", contentType)
	}

	switch mediaType {
	case "multipart/form-data":
		boundary := params["boundary"]
		if len(boundary) < minBoundaryLength {
			return statusBadRequest("invalid boundary %q", boundary)
		}
		return firmwareRequest(req.Body, boundary)
	default:
		return statusBadRequest("invalid media type %q", mediaType)
	}
}

// Writing files

type fileInfo struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
}

func firmwareRequest(body io.Reader, boundary string) Response {
	// Read metadata part (field name "request").
	mr := multipart.NewReader(body, boundary)
	part, err := mr.NextPart()
	if err != nil {
		return statusBadRequest("cannot read request metadata: %v", err)
	}
	if part.FormName() != "request" {
		return statusBadRequest(`metadata field name must be "request", got %q`, part.FormName())
	}

	// Decode metadata about files to write.
	var payload struct {
		Action string   `json:"action"`
		Slot   string   `json:"slot"`
		File   fileInfo `json:"file"`
	}
	decoder := json.NewDecoder(part)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request metadata: %v", err)
	}
	if payload.Action != "refresh" {
		return statusBadRequest(`multipart action must be "replace", got %q`, payload.Action)
	}
	if payload.File.Size == 0 {
		return statusBadRequest("empty file not valid")
	}

	// Receive the file
	part, err = mr.NextPart()
	if err != nil {
		return statusBadRequest("cannot read file part: %v", err)
	}
	if part.FormName() != "file" {
		return statusBadRequest(`field name must be "file", got %q`, part.FormName())
	}
	path := multipartFilename(part)
	if path != payload.File.Path {
		return statusBadRequest("no metadata for path %q", path)
	}
	err = writeSlotFile(payload.Slot, payload.File, part)
	part.Close()

	return SyncResponse(&fileResult{
		Path:  payload.File.Path,
		Error: fwErrorToResult(err),
	})
}

func fwErrorToResult(err error) *errorResult {
	if err == nil {
		return nil
	}
	return &errorResult{
		Kind:    errorKindGenericFileError,
		Message: err.Error(),
	}
}

func writeSlotFile(slot string, item fileInfo, source io.Reader) error {
	if pathpkg.IsAbs(item.Path) {
		return absolutePathError(item.Path)
	}

	// TODO: hack in path
	path := filepath.Join("/tmp", slot, item.Path)

	// Current user/group
	sysUid, sysGid := sys.UserID(osutil.NoChown), sys.GroupID(osutil.NoChown)

	// Create slot-relative directory if needed.
	err := mkdirAllChown(pathpkg.Dir(path), 0o664, sysUid, sysGid)
	if err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}

	// Atomically write file content to destination.
	return atomicWriteChown(path, source, 0o664, osutil.AtomicWriteChmod, sysUid, sysGid)
}

// Because it's hard to test os.Chown without running the tests as root.
var (
	atomicWrite = osutil.AtomicWrite
)
