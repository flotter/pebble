// Copyright (C) 2025 Canonical Ltd
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

package pairingstate_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/planstate"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type pairingSuite struct{}

var _ = Suite(&pairingSuite{})

func writeLayer(c *C, layersDir string, layer string, order int) {
	filename := fmt.Sprintf("%03[1]d-layer-file-%[1]d.yaml", order)
	err := os.WriteFile(filepath.Join(layersDir, filename), []byte(layer), 0o644)
	c.Assert(err, IsNil)
}

func planYAML(c *C, mgr *planstate.PlanManager) string {
	plan := mgr.Plan()
	yml, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	return string(yml)
}

// The YAML on tests passes through this function to deindent and
// replace tabs by spaces, so we can keep the code here sane.
func reindent(in string) []byte {
	var buf bytes.Buffer
	var trim string
	for _, line := range strings.Split(in, "\n") {
		if trim == "" {
			trimmed := strings.TrimLeft(line, "\t")
			if trimmed == "" {
				continue
			}
			if trimmed[0] == ' ' {
				panic("Tabs and spaces mixed early on string:\n" + in)
			}
			trim = line[:len(line)-len(trimmed)]
		}
		trimmed := strings.TrimPrefix(line, trim)
		if len(trimmed) == len(line) && strings.Trim(line, "\t ") != "" {
			panic("Line not indented consistently:\n" + line)
		}
		trimmed = strings.ReplaceAll(trimmed, "\t", "    ")
		buf.WriteString(trimmed)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
