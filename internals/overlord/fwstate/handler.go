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
	"fmt"
	"time"
	pathpkg "path"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
	"github.com/canonical/pebble/internals/overlord/state"
)

func (fm *FirmwareManager) doRefreshPrepare(task *state.Task, tomb *tomb.Tomb) error {
	time.Sleep(2 * time.Second)
	return nil
}

func (fm *FirmwareManager) undoRefreshPrepare(task *state.Task, tomb *tomb.Tomb) error {
	return nil
}

func (fm *FirmwareManager) doRefreshUpload(task *state.Task, tomb *tomb.Tomb) (err error) {
	var opts RefreshOptions

	fm.state.Lock()
	change := task.Change()
	changeId := change.ID()
	change.Get("firmware-refresh", &opts)
	fm.state.Unlock()

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()


	var req *UploadRequest
ready:
	for {
		req = fm.uploadRequest(changeId)
		if req != nil {
			// Client already supplied the io.Reader
			break ready
		}
		select {
		case <-ticker.C:
		case <-timeout:
			return fmt.Errorf("timeout waiting for client upload metadata")
		}
	}

	// If we exit, make sure the requesting side exits.
	defer func() {
		req.Done <- err
	}()

        // TODO: hack in path
        path := filepath.Join("/tmp", opts.Target, "firmware.img")

        // Current user/group
        sysUid, sysGid := sys.UserID(osutil.NoChown), sys.GroupID(osutil.NoChown)

        // Create slot-relative directory if needed.
        err = osutil.MkdirAllChown(pathpkg.Dir(path), 0o664, sysUid, sysGid)
        if err != nil {
                return fmt.Errorf("cannot create directory: %w", err)
        }

        aw, err := osutil.NewAtomicFile(path, 0o664, osutil.AtomicWriteChmod, sysUid, sysGid)
        if err != nil {
                return err
        }

        // Cancel once Committed is a NOP :-)
        defer aw.Cancel()

	blockSize := int64(4096)
	totalSize := req.Size
	doneSize := int64(0)

	fm.state.Lock()
		// TODO: progress not supporting int64
	task.SetProgress("firmware upload", int(doneSize), int(totalSize))
	fm.state.Unlock()

	for {
		n, err := io.CopyN(aw, req.Source, blockSize)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		doneSize += n

		fm.state.Lock()
		// TODO: progress not supporting int64
		task.SetProgress("firmware upload", int(doneSize), int(totalSize))
		fm.state.Unlock()

		time.Sleep(time.Millisecond)
	}

        return aw.Commit()
}

func (fm *FirmwareManager) undoRefreshUpload(task *state.Task, tomb *tomb.Tomb) error {
	return nil
}

func (fm *FirmwareManager) doRefreshComplete(task *state.Task, tomb *tomb.Tomb) error {
	time.Sleep(2 * time.Second)
	return nil
}

func (fm *FirmwareManager) undoRefreshComplete(task *state.Task, tomb *tomb.Tomb) error {
	return nil
}
