// Copyright (c) 2024 Canonical Ltd
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

package facet

import (
	"fmt"

	"github.com/canonical/pebble/internals/plan"
)

const Key plan.PartName = "log-targets"

func ToLogTargets(part plan.Part) (LogTargets, error) {
	facet, ok := part.(*Facet)
	if ok {
		return facet.LogTargets, nil
	}
	return nil, fmt.Errorf("cannot assert facet type as log targets")
}

// Facet creator for plan library
type FacetCreator struct{}

func (fc *FacetCreator) Key() plan.PartName {
	return Key
}

func (fc *FacetCreator) Wants() []plan.PartName {
	return []plan.PartName{}
}

func (fc *FacetCreator) New() plan.Part {
	return NewFacet()
}

type LogTargets map[string]*LogTarget

// Facet
type Facet struct { 
	LogTargets LogTargets `yaml:",inline"`
}

func NewFacet() *Facet {
	facet := &Facet{
		LogTargets: make(map[string]*LogTarget),
	}
	return facet
}

func (f *Facet) ValidatePart() error {
	// TODO
	return nil
}

func (f *Facet) ValidatePlan(combined *plan.Layer) error {
	// TODO
	return nil
}

func (f *Facet) Combine(other plan.Part) error {
	otherFacet, ok := other.(*Facet)
	if !ok {
		return fmt.Errorf("cannot combine incompatible facets")
	}

	for key, entry := range otherFacet.LogTargets {
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := f.LogTargets[key]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				f.LogTargets[key] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			f.LogTargets[key] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`LogTargets must define "override" for entry %q`, key),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`LogTargets has invalid "override" value for entry %q`, key),
			}
		}
	}
	return nil
}

func (f *Facet) IsNonEmpty() bool {
	return len(f.LogTargets) != 0
}



// LogTarget specifies a remote server to forward logs to.
type LogTarget struct {
	Name     string            `yaml:"-"`
	Type     LogTargetType     `yaml:"type"`
	Location string            `yaml:"location"`
	Services []string          `yaml:"services"`
	Override plan.Override          `yaml:"override,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
}

// LogTargetType defines the protocol to use to forward logs.
type LogTargetType string

const (
	LokiTarget     LogTargetType = "loki"
	SyslogTarget   LogTargetType = "syslog"
	UnsetLogTarget LogTargetType = ""
)

// Copy returns a deep copy of the log target configuration.
func (t *LogTarget) Copy() *LogTarget {
	copied := *t
	copied.Services = append([]string(nil), t.Services...)
	if t.Labels != nil {
		copied.Labels = make(map[string]string)
		for k, v := range t.Labels {
			copied.Labels[k] = v
		}
	}
	return &copied
}

// Merge merges the fields set in other into t.
func (t *LogTarget) Merge(other *LogTarget) {
	if other.Type != "" {
		t.Type = other.Type
	}
	if other.Location != "" {
		t.Location = other.Location
	}
	t.Services = append(t.Services, other.Services...)
	for k, v := range other.Labels {
		if t.Labels == nil {
			t.Labels = make(map[string]string)
		}
		t.Labels[k] = v
	}
}

//// -- Plan validation code
//
//		for name, target := range layer.LogTargets {
//			switch target.Override {
//			case MergeOverride:
//				if old, ok := combined.LogTargets[name]; ok {
//					copied := old.Copy()
//					copied.Merge(target)
//					combined.LogTargets[name] = copied
//					break
//				}
//				fallthrough
//			case ReplaceOverride:
//				combined.LogTargets[name] = target.Copy()
//			case UnknownOverride:
//				return nil, &FormatError{
//					Message: fmt.Sprintf(`layer %q must define "override" for log target %q`,
//						layer.Label, target.Name),
//				}
//			default:
//				return nil, &FormatError{
//					Message: fmt.Sprintf(`layer %q has invalid "override" value for log target %q`,
//						layer.Label, target.Name),
//				}
//			}
//		}
//	}
//
//
//
//	for name, target := range combined.LogTargets {
//		switch target.Type {
//		case LokiTarget, SyslogTarget:
//			// valid, continue
//		case UnsetLogTarget:
//			return nil, &FormatError{
//				Message: fmt.Sprintf(`plan must define "type" (%q or %q) for log target %q`,
//					LokiTarget, SyslogTarget, name),
//			}
//		default:
//			return nil, &FormatError{
//				Message: fmt.Sprintf(`log target %q has unsupported type %q, must be %q or %q`,
//					name, target.Type, LokiTarget, SyslogTarget),
//			}
//		}
//
//		// Validate service names specified in log target
//		for _, serviceName := range target.Services {
//			serviceName = strings.TrimPrefix(serviceName, "-")
//			if serviceName == "all" {
//				continue
//			}
//			if _, ok := combined.Services[serviceName]; ok {
//				continue
//			}
//			return nil, &FormatError{
//				Message: fmt.Sprintf(`log target %q specifies unknown service %q`,
//					target.Name, serviceName),
//			}
//		}
//
//		if target.Location == "" {
//			return nil, &FormatError{
//				Message: fmt.Sprintf(`plan must define "location" for log target %q`, name),
//			}
//		}
//	}
//
//
//// -- Facet validation code
//	
//for name, target := range layer.LogTargets {
//		if name == "" {
//			return nil, &FormatError{
//				Message: fmt.Sprintf("cannot use empty string as log target name"),
//			}
//		}
//		if target == nil {
//			return nil, &FormatError{
//				Message: fmt.Sprintf("log target object cannot be null for log target %q", name),
//			}
//		}
//		for labelName := range target.Labels {
//			// 'pebble_*' labels are reserved
//			if strings.HasPrefix(labelName, "pebble_") {
//				return nil, &FormatError{
//					Message: fmt.Sprintf(`log target %q: label %q uses reserved prefix "pebble_"`, name, labelName),
//				}
//			}
//		}
//		target.Name = name
//	}
