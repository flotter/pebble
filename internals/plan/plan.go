// Copyright (c) 2021 Canonical Ltd
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

package plan

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/x-go/strutil/shlex"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
)

// PartName uniquely describes a top-level part key, as used in the plan schema.
type PartName string

// PartType describes a specific part type.
type PartType interface {
	// The top level YAML key for this part.
	Key() PartName

	// The parts on which this part type is dependant. They have to be
	// available by the time this type is registered.
	Wants() (parts []PartName)

	// New create a new part instance.
	New(layer *Layer) Part
}

// Part defines an externally defined data structure that is compatible as
// part of the global plan.
type Part interface {
	// UnmarshalYAML loads a part from a YAML layer.
	UnmarshalYAML(value *yaml.Node) error

	// Copy creates a new deep copy of a part.
	Copy() Part

	// Merge can merge another part into itself, taking the override
	// preference into account, which would either override or merge
	// a part entry.
	Merge(other Part)
}

type Layer struct {
	Order         int               `yaml:"-"`
	Label         string            `yaml:"-"`
	Summary       string            `yaml:"summary,omitempty"`
	Description   string            `yaml:"description,omitempty"`

	Parts         map[PartName]Part `yaml:,inline`
	PartsOrder    []PartName        `yaml:"-"`
}

type Plan struct {
	baseDir           string
	orderedPartTypes  []PartType
	
	Layers            []*Layer
	Combined          *Layer
}

func NewPlan(baseDir string) *Plan {
	return &Plan{
		baseDir: baseDir,
	}
}

// AddPartType adds a new part schema to the global plan. It returns an
// error if the part dependencies (other parts) are not already added.
//
// The design here assumes a static global plan part add order. In other
// words, a part x in Pebble that depends on part y for validation, must
// be added after the dependency was added.
func (p *Plan) AddPartType(partType PartType) error {
	p.orderedPartTypes = append(p.orderedPartTypes, partType)

	prevNames := []PartName{}
	for _, partType := range p.orderedPartTypes {
		for _, want := range partType.Wants() {
			for _, prev := range prevNames {
				if want == prev {
					break
				}
			}
			return fmt.Errorf("cannot find plan part dependency %s", want)
		}
		prevTypes = append(prevTypes, partType.Key())
	}
	return nil
}

// Load reads the configuration layers from the "layers" sub-directory in
// baseDir, and updates the plan, dropping any previous ephermeral layers. If
// the "layers" sub-directory doesn't exist, it returns a valid Plan with
// no layers.
func (p *Plan) Load() error {
	layersDir := filepath.Join(dir, "layers")
	_, err := os.Stat(layersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	plan.Layers, err := ReadLayersDir(layersDir)
	if err != nil {
		return err
	}
	plan.Combined, err := CombineLayers(layers...)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plan) NewLayer() *Layer {
	layer := &Layer{
		Parts: make(map[string]LayerPart),
	}
	for k, v := range p.layerParts {
		layer.Parts[k] = v(layer)
	}
	return layer
}

func (l *Layer) ParseLayer(order int, label string, data []byte) error {
	dec := yaml.NewDecoder(bytes.NewBuffer(data))
	dec.KnownFields(true)
	err := dec.Decode(l)
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}
	layer.Order = order
	layer.Label = label

	for name, part := range l.Parts {
		err := part.Validate()
		if err != nil {
			return fmt.Errorf("cannot validate %s part: %w", name, err)
		}
	}

	for name, service := range layer.Services {
		if name == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use empty string as service name"),
			}
		}
		if name == "pebble" {
			// Disallow service name "pebble" to avoid ambiguity (for example,
			// in log output).
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use reserved service name %q", name),
			}
		}
		// Deprecated service names
		if name == "all" || name == "default" || name == "none" {
			logger.Noticef("Using keyword %q as a service name is deprecated", name)
		}
		if strings.HasPrefix(name, "-") {
			return nil, &FormatError{
				Message: fmt.Sprintf(`cannot use service name %q: starting with "-" not allowed`, name),
			}
		}
		if service == nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("service object cannot be null for service %q", name),
			}
		}
		service.Name = name
	}

	for name, check := range layer.Checks {
		if name == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use empty string as check name"),
			}
		}
		if check == nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("check object cannot be null for check %q", name),
			}
		}
		check.Name = name
	}

	for name, target := range layer.LogTargets {
		if name == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use empty string as log target name"),
			}
		}
		if target == nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("log target object cannot be null for log target %q", name),
			}
		}
		for labelName := range target.Labels {
			// 'pebble_*' labels are reserved
			if strings.HasPrefix(labelName, "pebble_") {
				return nil, &FormatError{
					Message: fmt.Sprintf(`log target %q: label %q uses reserved prefix "pebble_"`, name, labelName),
				}
			}
		}
		target.Name = name
	}

	err = layer.checkCycles()
	if err != nil {
		return nil, err
	}
	return &layer, err
}

// FormatError is the error returned when a layer has a format error, such as
// a missing "override" field.
type FormatError struct {
	Message string
}

func (e *FormatError) Error() string {
	return e.Message
}

// CombineLayers combines the given layers into a single layer, with the later
// layers overriding earlier ones.
func CombineLayers(layers ...*Layer) (*Layer, error) {
	combined := &Layer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
	}
	if len(layers) == 0 {
		return combined, nil
	}
	last := layers[len(layers)-1]
	combined.Summary = last.Summary
	combined.Description = last.Description
	for _, layer := range layers {
		for name, service := range layer.Services {
			switch service.Override {
			case MergeOverride:
				if old, ok := combined.Services[name]; ok {
					copied := old.Copy()
					copied.Merge(service)
					combined.Services[name] = copied
					break
				}
				fallthrough
			case ReplaceOverride:
				combined.Services[name] = service.Copy()
			case UnknownOverride:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q must define "override" for service %q`,
						layer.Label, service.Name),
				}
			default:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q has invalid "override" value for service %q`,
						layer.Label, service.Name),
				}
			}
		}

		for name, check := range layer.Checks {
			switch check.Override {
			case MergeOverride:
				if old, ok := combined.Checks[name]; ok {
					copied := old.Copy()
					copied.Merge(check)
					combined.Checks[name] = copied
					break
				}
				fallthrough
			case ReplaceOverride:
				combined.Checks[name] = check.Copy()
			case UnknownOverride:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q must define "override" for check %q`,
						layer.Label, check.Name),
				}
			default:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q has invalid "override" value for check %q`,
						layer.Label, check.Name),
				}
			}
		}

		for name, target := range layer.LogTargets {
			switch target.Override {
			case MergeOverride:
				if old, ok := combined.LogTargets[name]; ok {
					copied := old.Copy()
					copied.Merge(target)
					combined.LogTargets[name] = copied
					break
				}
				fallthrough
			case ReplaceOverride:
				combined.LogTargets[name] = target.Copy()
			case UnknownOverride:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q must define "override" for log target %q`,
						layer.Label, target.Name),
				}
			default:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q has invalid "override" value for log target %q`,
						layer.Label, target.Name),
				}
			}
		}
	}

	// Ensure fields in combined layers validate correctly (and set defaults).
	for name, service := range combined.Services {
		if service.Command == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "command" for service %q`, name),
			}
		}
		_, _, err := service.ParseCommand()
		if err != nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q command invalid: %v", name, err),
			}
		}
		if !validServiceAction(service.OnSuccess, ActionFailureShutdown) {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q on-success action %q invalid", name, service.OnSuccess),
			}
		}
		if !validServiceAction(service.OnFailure, ActionSuccessShutdown) {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q on-failure action %q invalid", name, service.OnFailure),
			}
		}
		for _, action := range service.OnCheckFailure {
			if !validServiceAction(action, ActionSuccessShutdown) {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan service %q on-check-failure action %q invalid", name, action),
				}
			}
		}
		if !service.BackoffDelay.IsSet {
			service.BackoffDelay.Value = defaultBackoffDelay
		}
		if !service.BackoffFactor.IsSet {
			service.BackoffFactor.Value = defaultBackoffFactor
		} else if service.BackoffFactor.Value < 1 {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q backoff-factor must be 1.0 or greater, not %g", name, service.BackoffFactor.Value),
			}
		}
		if !service.BackoffLimit.IsSet {
			service.BackoffLimit.Value = defaultBackoffLimit
		}

	}

	for name, check := range combined.Checks {
		if check.Level != UnsetLevel && check.Level != AliveLevel && check.Level != ReadyLevel {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan check %q level must be "alive" or "ready"`, name),
			}
		}
		if !check.Period.IsSet {
			check.Period.Value = defaultCheckPeriod
		} else if check.Period.Value == 0 {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan check %q period must not be zero", name),
			}
		}
		if !check.Timeout.IsSet {
			check.Timeout.Value = defaultCheckTimeout
		} else if check.Timeout.Value == 0 {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan check %q timeout must not be zero", name),
			}
		} else if check.Timeout.Value >= check.Period.Value {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan check %q timeout must be less than period", name),
			}
		}
		if check.Threshold == 0 {
			// Default number of failures in a row before check triggers
			// action, default is >1 to avoid flapping due to glitches. For
			// what it's worth, Kubernetes probes uses a default of 3 too.
			check.Threshold = defaultCheckThreshold
		}

		numTypes := 0
		if check.HTTP != nil {
			if check.HTTP.URL == "" {
				return nil, &FormatError{
					Message: fmt.Sprintf(`plan must set "url" for http check %q`, name),
				}
			}
			numTypes++
		}
		if check.TCP != nil {
			if check.TCP.Port == 0 {
				return nil, &FormatError{
					Message: fmt.Sprintf(`plan must set "port" for tcp check %q`, name),
				}
			}
			numTypes++
		}
		if check.Exec != nil {
			if check.Exec.Command == "" {
				return nil, &FormatError{
					Message: fmt.Sprintf(`plan must set "command" for exec check %q`, name),
				}
			}
			_, err := shlex.Split(check.Exec.Command)
			if err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan check %q command invalid: %v", name, err),
				}
			}
			_, contextExists := combined.Services[check.Exec.ServiceContext]
			if check.Exec.ServiceContext != "" && !contextExists {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan check %q service context specifies non-existent service %q",
						name, check.Exec.ServiceContext),
				}
			}
			_, _, err = osutil.NormalizeUidGid(check.Exec.UserID, check.Exec.GroupID, check.Exec.User, check.Exec.Group)
			if err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan check %q has invalid user/group: %v", name, err),
				}
			}
			numTypes++
		}
		if numTypes != 1 {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must specify one of "http", "tcp", or "exec" for check %q`, name),
			}
		}
	}

	for name, target := range combined.LogTargets {
		switch target.Type {
		case LokiTarget, SyslogTarget:
			// valid, continue
		case UnsetLogTarget:
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "type" (%q or %q) for log target %q`,
					LokiTarget, SyslogTarget, name),
			}
		default:
			return nil, &FormatError{
				Message: fmt.Sprintf(`log target %q has unsupported type %q, must be %q or %q`,
					name, target.Type, LokiTarget, SyslogTarget),
			}
		}

		// Validate service names specified in log target
		for _, serviceName := range target.Services {
			serviceName = strings.TrimPrefix(serviceName, "-")
			if serviceName == "all" {
				continue
			}
			if _, ok := combined.Services[serviceName]; ok {
				continue
			}
			return nil, &FormatError{
				Message: fmt.Sprintf(`log target %q specifies unknown service %q`,
					target.Name, serviceName),
			}
		}

		if target.Location == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "location" for log target %q`, name),
			}
		}
	}

	// Ensure combined layers don't have cycles.
	err := combined.checkCycles()
	if err != nil {
		return nil, err
	}

	return combined, nil
}

func (l *Layer) checkCycles() error {
	var names []string
	for name := range l.Services {
		names = append(names, name)
	}
	_, err := order(l.Services, names, false)
	return err
}


var fnameExp = regexp.MustCompile("^([0-9]{3})-([a-z](?:-?[a-z0-9]){2,}).yaml$")

func ReadLayersDir(dirname string) ([]*Layer, error) {
	finfos, err := ioutil.ReadDir(dirname)
	if err != nil {
		// Errors from package os generally include the path.
		return nil, fmt.Errorf("cannot read layers directory: %v", err)
	}

	orders := make(map[int]string)
	labels := make(map[string]int)

	// Documentation says ReadDir result is already sorted by name.
	// This is fundamental here so if reading changes make sure the
	// sorting is preserved.
	var layers []*Layer
	for _, finfo := range finfos {
		if finfo.IsDir() || !strings.HasSuffix(finfo.Name(), ".yaml") {
			continue
		}
		// TODO Consider enforcing permissions and ownership here to
		//      avoid mistakes that could lead to hacks.
		match := fnameExp.FindStringSubmatch(finfo.Name())
		if match == nil {
			return nil, fmt.Errorf("invalid layer filename: %q (must look like \"123-some-label.yaml\")", finfo.Name())
		}

		data, err := ioutil.ReadFile(filepath.Join(dirname, finfo.Name()))
		if err != nil {
			// Errors from package os generally include the path.
			return nil, fmt.Errorf("cannot read layer file: %v", err)
		}
		label := match[2]
		order, err := strconv.Atoi(match[1])
		if err != nil {
			panic(fmt.Sprintf("internal error: filename regexp is wrong: %v", err))
		}

		oldLabel, dupOrder := orders[order]
		oldOrder, dupLabel := labels[label]
		if dupOrder {
			oldOrder = order
		} else if dupLabel {
			oldLabel = label
		}
		if dupOrder || dupLabel {
			return nil, fmt.Errorf("invalid layer filename: %q not unique (have \"%03d-%s.yaml\" already)", finfo.Name(), oldOrder, oldLabel)
		}

		orders[order] = label
		labels[label] = order

		layer, err := ParseLayer(order, label, data)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return layers, nil
}
