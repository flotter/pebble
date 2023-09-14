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
	"fmt"

	"github.com/canonical/pebble/internals/overlord/state"
)

type FirmwareManager struct{
	state *state.State

	uploadLock sync.Mutex
	uploadMap map[string]*UploadRequest
}

func NewFirmwareManager(s *state.State, runner *state.TaskRunner) *FirmwareManager {
	fm := &FirmwareManager{
		state: s,
		uploadMap: make(map[string]*UploadRequest),
	}

	runner.AddHandler("firmware-refresh-prepare", fm.doRefreshPrepare, fm.undoRefreshPrepare)
	runner.AddHandler("firmware-refresh-upload", fm.doRefreshUpload, fm.undoRefreshUpload)
	runner.AddHandler("firmware-refresh-complete", fm.doRefreshComplete, fm.undoRefreshComplete)

	return fm
}

type UploadRequest struct {
	Size int64
	Source io.Reader
	Done chan error
}

func (fm *FirmwareManager) uploadRequest(changeId string) *UploadRequest {
	fm.uploadLock.Lock()
	defer fm.uploadLock.Unlock()
	if req, ok := fm.uploadMap[changeId]; ok {
		return req
	}
	return nil
}

func (fm *FirmwareManager) SetUploadRequest(changeId string, req *UploadRequest) {
	fm.uploadLock.Lock()
	defer fm.uploadLock.Unlock()
	fm.uploadMap[changeId] = req
}

func (fm *FirmwareManager) Ensure() error {
	return nil
}

func (fm *FirmwareManager) Stop() {
}

func (fm *FirmwareManager) RunningSlot() string {
	// TODO: The bootloader should be consulted
	return "a"
}

func GetInstallSlot(slot string) (target string, err error) {
	switch slot {
	case "a":
		target = "b"
	case "b":
		target = "a"
	default:
		err = fmt.Errorf("unsupported slot %v", slot)
	}

	return target, err
}
