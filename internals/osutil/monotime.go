// Copyright (c) 2025 Canonical Ltd
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

package osutil

import (
	_ "unsafe"
)

// GetLinuxKernelRuntime returns the nanosecond duration since the start
// of the Linux kernel. This function uses CLOCK_MONOTONIC under the hood
// but binds on top of functionality already in the Golang runtime, but
// not publically exposed.
//
// This function is not a replacement for the time package, which already
// provides duration based on CLOCK_MONOTONIC. This is exclusively
// intended for cases where time elapse since the Linux kernel start
// is required.
//
//go:noescape
//go:linkname GetLinuxKernelRuntime runtime.nanotime
func GetLinuxKernelRuntime() int64
