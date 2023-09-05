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
	"io"
	"sync"

	"github.com/canonical/pebble/internals/overlord/state"
)

type FirmwareManager struct{
	uploadLock sync.Mutex
	uploadMap map[string]*UploadRequest
}

func NewFirmwareManager(s *state.State, runner *state.TaskRunner) *FirmwareManager {
	fm := &FirmwareManager{
		uploadMap: make(map[string]*UploadRequest),
	}

	runner.AddHandler("firmware-upload", fm.doUpload, nil)

	return fm
}

type UploadRequest struct {
	Size int64
	Source io.Reader
	Done chan error
}

func (fw *FirmwareManager) uploadRequest(changeId string) *UploadRequest {
	fw.uploadLock.Lock()
	defer fw.uploadLock.Unlock()
	if req, ok := fw.uploadMap[changeId]; ok {
		return req
	}
	return nil
}

func (fw *FirmwareManager) SetUploadRequest(changeId string, req *UploadRequest) {
	fw.uploadLock.Lock()
	defer fw.uploadLock.Unlock()
	fw.uploadMap[changeId] = req
}

func (fm *FirmwareManager) Ensure() error {
	return nil
}

func (fm *FirmwareManager) Stop() {
}
