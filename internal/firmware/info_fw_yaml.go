// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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

package firmware

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

type fwYaml struct {
	Name            string                 `yaml:"name"`
	Version         string                 `yaml:"version"`
	Description     string                 `yaml:"description"`
	Summary         string                 `yaml:"summary"`
	Base            string                 `yaml:"base,omitempty"`
}

// InfoFromFwYaml initializes the YAML supplied part of firmware Info
func infoFromFwYaml(yamlData []byte) (*Info, error) {
	var y fwYaml
	err := yaml.Unmarshal(yamlData, &y)
	if err != nil {
		return nil, fmt.Errorf("cannot parse firmware.yaml: %s", err)
	}
	fw := &Info{
		Version:             y.Version,
		OriginalName:        y.Name,
		OriginalDescription: y.Description,
		OriginalSummary:     y.Summary,
		Base:                y.Base,
	}
	return fw, nil
}
