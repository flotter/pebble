// Copyright (c) 2020 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/plan"
)

// planInput can be a file input, or an input via the
// API for either a layer append or layer update
type planInput struct {
	order        int
	label        string
	yaml         string
	updatedParts []string // Only considered for appends and udpates
}

// planResult represents the combined plan
type planResult struct {
	order   int
	label   string
	summary string
	desc    string
	x       *PartX
	y       *PartY
}

var planTests = []struct {
	planTypes        []plan.PartType
	files            []*planInput
	fileUpdatedParts []string
	appends          []*planInput
	updates          []*planInput

	result      *planResult
	errorString string
}{
	// Index 0: No Parts
	{},
	// Index 1: Invalid Part order
	{
		planTypes: []plan.PartType{
			&partXType{},
			&partYType{},
		},
		errorString: "cannot find plan part dependency .*",
	},
	// Index 2: Correct Part order
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
	},
	// Index 3: Load file layers invalid part
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-xy",
				yaml: `
					summary: xy
					description: desc
					invalid:
				`,
			},
		},
		errorString: "cannot parse layer .*: cannot find plan part .*",
	},
	// Index 4: Load file layers not unique order
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-1",
				yaml: `
					summary: xy
					description: desc
				`,
			},
			&planInput{
				order: 1,
				label: "layer-2",
				yaml: `
					summary: xy
					description: desc
				`,
			},
		},
		errorString: "invalid layer filename: .* not unique .*",
	},
	// Index 5: Load file layers not unique label
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-xy",
				yaml: `
					summary: xy
					description: desc
				`,
			},
			&planInput{
				order: 2,
				label: "layer-xy",
				yaml: `
					summary: xy
					description: desc
				`,
			},
		},
		errorString: "invalid layer filename: .* not unique .*",
	},
	// Index 6: Load file layers with part validation failure
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x:
						z1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		errorString: "cannot validate part .*",
	},
	// Index 7: Load file layers failed plan validation
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x:
						x1:
							override: replace
							a: a
							b: b
							y:
							  - y2
				`,
			},
			&planInput{
				order: 2,
				label: "layer-y",
				yaml: `
					summary: y
					description: desc-y
					y:
						y1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		errorString: "cannot validate plan part .*",
	},
	// Index 8: Load file layers
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x:
						x1:
							override: replace
							a: a
							b: b
							y:
							  - y1
				`,
			},
			&planInput{
				order: 2,
				label: "layer-y",
				yaml: `
					summary: y
					description: desc-y
					y:
						y1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		result: &planResult{
			summary: "y",
			desc:    "desc-y",
			x: &PartX{
				Entries: map[string]*X{
					"x1": &X{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
						Y: []string{
							"y1",
						},
					},
				},
			},
			y: &PartY{
				Entries: map[string]*Y{
					"y1": &Y{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
					},
				},
			},
		},
	},
	// Index 9: Load file layers with appends
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x:
						x1:
							override: replace
							a: a
							b: b
							y:
							  - y1
				`,
			},
			&planInput{
				order: 2,
				label: "layer-y",
				yaml: `
					summary: y
					description: desc-y
					y:
						y1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		appends: []*planInput{
			&planInput{
				order: 0,
				label: "append-x",
				yaml: `
					summary: x-append
					description: desc-x
				`,
			},
			&planInput{
				order: 0,
				label: "append-y",
				yaml: `
					summary: y-append
					description: desc-y
					x:
						x1:
							override: merge
							y:
							  - y2
					y:
						y2:
							override: replace
							a: a
							b: b
				`,
			},
		},
		result: &planResult{
			summary: "y-append",
			desc:    "desc-y",
			x: &PartX{
				Entries: map[string]*X{
					"x1": &X{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
						Y: []string{
							"y1",
							"y2",
						},
					},
				},
			},
			y: &PartY{
				Entries: map[string]*Y{
					"y1": &Y{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
					},
					"y2": &Y{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
					},
				},
			},
		},
	},
	// Index 10: Load file layers with appends and updates
	{
		planTypes: []plan.PartType{
			&partYType{},
			&partXType{},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x:
						x1:
							override: replace
							a: a
							b: b
							y:
							  - y1
				`,
			},
			&planInput{
				order: 2,
				label: "layer-y",
				yaml: `
					summary: y
					description: desc-y
					y:
						y1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		appends: []*planInput{
			&planInput{
				order: 0,
				label: "append-x",
				yaml: `
					summary: x-append
					description: desc-x
				`,
			},
			&planInput{
				order: 0,
				label: "append-y",
				yaml: `
					summary: y-append
					description: desc-y
					x:
						x1:
							override: merge
							y:
							  - y2
					y:
						y2:
							override: replace
							a: a
							b: b
				`,
			},
		},
		updates: []*planInput{
			&planInput{
				order: 0,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x:
						x1:
							override: replace
							a: c
				`,
			},
		},
		result: &planResult{
			summary: "y-append",
			desc:    "desc-y",
			x: &PartX{
				Entries: map[string]*X{
					"x1": &X{
						Override: plan.ReplaceOverride,
						A:        "c",
						Y: []string{
							"y2",
						},
					},
				},
			},
			y: &PartY{
				Entries: map[string]*Y{
					"y1": &Y{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
					},
					"y2": &Y{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
					},
				},
			},
		},
	},
}

func (s *S) TestPlan(c *C) {
	for testIndex, planTest := range planTests {
		c.Logf("Running TestPlan() with test data index %v", testIndex)

		baseDir := c.MkDir()

		// Write all the YAML data to disk in a temporary location
		s.writeLayerFiles(c, baseDir, planTest.files)

		p := plan.NewPlan(baseDir)

		fail := func() error {
			var err error

			// Add types
			for _, partType := range planTest.planTypes {
				if err = p.AddPartType(partType); err != nil {
					return err
				}
			}

			// Load the plan layers
			_, err = p.Load()
			if err != nil {
				return err
			}

			// Process the appends
			for _, layer := range planTest.appends {
				newLayer, err := p.ParseLayer(layer.order, layer.label, reindent(layer.yaml))
				if err != nil {
					return err
				}
				_, err = p.AppendLayer(newLayer)
				if err != nil {
					return err
				}
			}

			// Process the updates
			for _, layer := range planTest.updates {
				newLayer, err := p.ParseLayer(layer.order, layer.label, reindent(layer.yaml))
				if err != nil {
					return err
				}
				_, err = p.UpdateLayer(newLayer)
				if err != nil {
					return err
				}
			}

			return nil
		}()

		if fail != nil {
			c.Assert(fail, ErrorMatches, planTest.errorString)
		}

		// Check the plan against the test result
		if planTest.result != nil {
			c.Assert(p.Summary(), Equals, planTest.result.summary)
			c.Assert(p.Description(), Equals, planTest.result.desc)

			// PartX
			partX, err := p.Part(XKey)
			c.Assert(err, IsNil)
			concreteX, err := ToPartX(partX)
			c.Assert(err, IsNil)
			c.Assert(concreteX.Entries, DeepEquals, planTest.result.x.Entries)

			// PartY
			partY, err := p.Part(YKey)
			c.Assert(err, IsNil)
			concreteY, err := ToPartY(partY)
			c.Assert(err, IsNil)
			c.Assert(concreteY.Entries, DeepEquals, planTest.result.y.Entries)
		}
	}
}

// Part X source file

// Validation of X depend on access to Y.

const XKey plan.PartName = "x"

func ToPartX(part plan.Part) (*PartX, error) {
	partType, ok := part.(*PartX)
	if ok {
		return partType, nil
	}
	return nil, fmt.Errorf("cannot assert part type as PartX")
}

// Part X
type partXType struct{}

func (x partXType) Key() plan.PartName {
	return XKey
}

func (x partXType) Wants() []plan.PartName {
	return []plan.PartName{YKey}
}

func (x partXType) New() plan.Part {
	return NewPartX()
}

type PartX struct {
	Entries map[string]*X `yaml:",inline"`
}

func NewPartX() *PartX {
	part := &PartX{
		Entries: make(map[string]*X),
	}
	return part
}

func (part *PartX) ValidatePart() error {
	for key, _ := range part.Entries {
		// Test requirement: keys must start with x
		if !strings.HasPrefix(key, "x") {
			return fmt.Errorf("part x keys must start with x")
		}
	}
	return nil
}

func (part *PartX) ValidatePlan(combined *plan.Layer) error {
	// We need to consult Part Y for complete validation as Part X
	// depends on Part Y.
	partY, err := combined.Part(YKey)
	if err != nil {
		return err
	}
	concreteY, err := ToPartY(partY)
	if err != nil {
		return err
	}

	// Make sure every Y key in X refer to an existing Y entry.
	for keyX, entryX := range part.Entries {
		for _, refY := range entryX.Y {
			found := false
			for keyY, _ := range concreteY.Entries {
				if refY == keyY {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("cannot find y entry %v as required by part x entry %v ", refY, keyX)
			}
		}
	}

	return nil
}

func (part *PartX) Combine(other plan.Part) error {
	otherPartX, ok := other.(*PartX)
	if !ok {
		return fmt.Errorf("cannot combine provided part with PartX: invalid part type")
	}

	for key, entry := range otherPartX.Entries {
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := part.Entries[key]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				part.Entries[key] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			part.Entries[key] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`part X must define "override" for entry %q`, key),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`part X has invalid "override" value for entry %q`, key),
			}
		}
	}
	return nil
}

func (part *PartX) IsNonEmpty() bool {
	return len(part.Entries) != 0
}

type X struct {
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`
	A        string        `yaml:"a,omitempty"`
	B        string        `yaml:"b"`
	Y        []string      `yaml:"y,omitempty"`
}

func (x *X) Copy() *X {
	copied := *x
	copied.Y = append([]string(nil), x.Y...)
	return &copied
}

func (x *X) Merge(other *X) {
	if other.A != "" {
		x.A = other.A
	}
	if other.B != "" {
		x.B = other.B
	}
	x.Y = append(x.Y, other.Y...)
}

// Part Y source file

// Validation of Y has no dependencies on other Parts.

const YKey plan.PartName = "y"

func ToPartY(part plan.Part) (*PartY, error) {
	partType, ok := part.(*PartY)
	if ok {
		return partType, nil
	}
	return nil, fmt.Errorf("cannot assert part type as PartY")
}

// Part Y
type partYType struct{}

func (y partYType) Key() plan.PartName {
	return YKey
}

func (y partYType) Wants() []plan.PartName {
	return []plan.PartName{}
}

func (y partYType) New() plan.Part {
	return NewPartY()
}

type PartY struct {
	Entries map[string]*Y `yaml:",inline"`
}

func NewPartY() *PartY {
	part := &PartY{
		Entries: make(map[string]*Y),
	}
	return part
}

func (part *PartY) ValidatePart() error {
	for key, _ := range part.Entries {
		// Test requirement: keys must start with y
		if !strings.HasPrefix(key, "y") {
			return fmt.Errorf("part y keys must start with y")
		}
	}
	return nil
}

func (part *PartY) ValidatePlan(combined *plan.Layer) error {
	return nil
}

func (part *PartY) Combine(other plan.Part) error {
	otherPartY, ok := other.(*PartY)
	if !ok {
		return fmt.Errorf("cannot combine provided part with PartY: invalid part type")
	}

	for key, entry := range otherPartY.Entries {
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := part.Entries[key]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				part.Entries[key] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			part.Entries[key] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`part Y must define "override" for entry %q`, key),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`part Y has invalid "override" value for entry %q`, key),
			}
		}
	}
	return nil
}

func (part *PartY) IsNonEmpty() bool {
	return len(part.Entries) != 0
}

type Y struct {
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`
	A        string        `yaml:"a,omitempty"`
	B        string        `yaml:"b"`
}

func (y *Y) Copy() *Y {
	copied := *y
	return &copied
}

func (y *Y) Merge(other *Y) {
	if other.A != "" {
		y.A = other.A
	}
	if other.B != "" {
		y.B = other.B
	}
}

// Test helper functions

// The YAML on tests below passes through this function to deindent and
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

func (s *S) writeLayerFiles(c *C, baseDir string, inputs []*planInput) {
	layersDir := filepath.Join(baseDir, "layers")
	err := os.MkdirAll(layersDir, 0755)
	c.Assert(err, IsNil)

	for _, input := range inputs {
		err := ioutil.WriteFile(filepath.Join(layersDir, fmt.Sprintf("%03d-%s.yaml", input.order, input.label)), reindent(input.yaml), 0644)
		c.Assert(err, IsNil)
	}
}
