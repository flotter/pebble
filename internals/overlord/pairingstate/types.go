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

package pairingstate

import (
	"encoding/json"
	"fmt"
	"time"
)

// StringDuration is a new type based on time.Duration.
type StringDuration time.Duration

// UnmarshalJSON implements the json.Unmarshaler interface.
// This method defines how to convert a JSON string into our StringDuration type.
func (d *StringDuration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration should be a string, got %s", data)
	}

	parsedDuration, err := time.ParseDuration(s)
	if err != nil {
		return err
	}

	*d = StringDuration(parsedDuration)
	return nil
}

func (d *StringDuration) Duration() time.Duration {
	return time.Duration(*d)
}
