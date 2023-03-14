// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package backend

// SetBootSlot change the bootloader configuration so that
// the next reboot issued will boot the selected firmware slot.
func (b Backend) SetBootSlot(bootSlot Bootslot) error {
	return nil
}

// GetBootSlot returns the slot currently selected for boot. This is not
// necessarily the running slot, as the boot slot may have been changed
// already, before any reboot was issued to switch.
func (b Backend) GetBootSlot() Bootslot, error {
	return fwstate.KernosA, nil
}

// GetRunningBootSlot returns the currently running boot slot.
func (b Backend) GetRunningBootSlot() Bootslot, error {
	return fwstate.KernosA, nil
}
