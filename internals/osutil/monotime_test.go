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

package osutil_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/osutil"
)

type monotimeSuite struct{}

var _ = Suite(&monotimeSuite{})

func (m *monotimeSuite) TestGetLinuxKernelRuntime(c *C) {
	m1Nano := osutil.GetLinuxKernelRuntime()
	time.Sleep(time.Second)
	m2Nano := osutil.GetLinuxKernelRuntime()

	startSeconds := m1Nano / 1000000000
	endSeconds := m2Nano / 1000000000

	// Make sure this value reflects monotonic time since the host machine booted. Let's
	// just assume we would not get here within the first 5 seconds from boot.
	if startSeconds <= 5 {
		c.Errorf("Start monotonic time for test does not indicate time since boot")
	}

	// Make sure if we read the time multiple times, we get unique values.
	if (endSeconds - startSeconds) < 1 {
		c.Errorf("Monotonic duration invalid")
	}
}
