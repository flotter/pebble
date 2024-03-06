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

const (
	defaultBackoffDelay  = 500 * time.Millisecond
	defaultBackoffFactor = 2.0
	defaultBackoffLimit  = 30 * time.Second

	defaultCheckPeriod    = 10 * time.Second
	defaultCheckTimeout   = 3 * time.Second
	defaultCheckThreshold = 3
)

// LayerSectionExtension allows the plan layer schema to evolve without
// adding centralised schema knowledge to the plan library.
type LayerSectionExtension interface {

	// REVIEW NOTE #1: The plan ParseLayer can handle section create plus
	// unmarshall, but this does not work great for plan Combine. The combined
	// layer is created empty first, and each section is iterated over,
	// merging the sections, one layer at a time. Point being, we cannot know
	// in advance easily which sections are used, so we create all of
	// them first which also allows foor consistent iterating loops over layer
	// sections (the basic flow is create an empty layer with all the
	// sections, and then either combine or unmarshall).

	// EmptySection produces an empty layer section.
	EmptySection(layer *Layer) LayerSection

	// REVIEW NOTE #2: ParseSection, still with the original API, also creates
	// an empty layer. I propose we remove that, passing in an empty layer
	// instead, which would make the combine and parse code look more consistent.

	// REVIEW NOTE #3: Have a look at the implementation of the test code, but
	// is there much value in having the unmarshalling code here, as opposed to
	// the concrete type itself. It feels a bit more elegant to ask the type
	// itself to marshal and unmarshal.

	// ParseSection takes YAML data associated with a section and populates
	// the section fields (unmarshalling, but also additional requirements
	// such as non-YAML fields requiring custom handling).
	ParseSection(layer *Layer, data *yaml.Node) (LayerSection, error)
}

type LayerSection interface {
	// Combine a layer section into itself, respecting schema merging features
	// such as override attributes (see type Override).
	Combine(section LayerSection) error

	// Validate this section in the context of the layer. Since sections can
	// refer to other sections, validation should only be performed on the
	// combined plan layer. A layer containing only section snippets, for
	// example to change a section attribute using MergeOVerride, may not
	// include referenced sections needed for validation.
	Validate() error

	// REVIEW NOTE #4: I added this to allow supporting omit at the marshal
	// time when dealing with sections. I need to know if its empty, which
	// I cannot know from this side.

	// IsEmpty returns true if the layer section is empty. This allows the
	// Marshalling process to omit it completely.
	IsEmpty() bool
}

type Plan struct {
	layerExtensions map[string]LayerSectionExtension `yaml:"-"`

	Layers   []*Layer `yaml:"-"`
	Combined *Layer   `yaml:"-"`
}

func NewPlan() *Plan {
	p := &Plan{
		layerExtensions: make(map[string]LayerSectionExtension),
	}
	return p
}

// AddSectionExtension extends plan layers with an additional schema section.
func (p *Plan) AddSectionExtension(field string, ext LayerSectionExtension) {
	p.layerExtensions[field] = ext
}

// Plan returns the combined layer
func (p *Plan) Plan() *Layer {
	return p.Combined
}

// Yaml returns the combined layer in YAML
func (p *Plan) Yaml() (out []byte, err error) {
	yamlMap := make(map[string]interface{})

	// REVIEW NOTE #7: Summary and description are discarded at the plan level.
	// I suspect we need this to stau backwards compatible ?

	if len(p.Combined.Services) != 0 {
		yamlMap["services"] = &p.Combined.Services
	}
	if len(p.Combined.LogTargets) != 0 {
		yamlMap["log-targets"] = &p.Combined.LogTargets
	}
	if len(p.Combined.Checks) != 0 {
		yamlMap["checks"] = &p.Combined.Checks
	}
	// Sections
	for key, ext := range p.layerExtensions {

		// REVIEW NOTE #8: We do not have the ability to locate a section from
		// a key in a nice way.

		ls := ext.EmptySection(p.Combined)
		for _, section := range p.Combined.Sections {
			if reflect.TypeOf(ls) == reflect.TypeOf(section) {
				// Omit if empty
				if !section.IsEmpty() {
					yamlMap[key] = section
				}
				break
			}
		}
	}
	return yaml.Marshal(yamlMap)
}

type Layer struct {
	Order       int                   `yaml:"-"`
	Label       string                `yaml:"-"`
	Summary     string                `yaml:"summary,omitempty"`
	Description string                `yaml:"description,omitempty"`
	Services    map[string]*Service   `yaml:"services,omitempty"`
	Checks      map[string]*Check     `yaml:"checks,omitempty"`
	LogTargets  map[string]*LogTarget `yaml:"log-targets,omitempty"`

	// REVIEW NOTE #5: I think there are a few benefits in having the
	// section key available in the layer itself. Currently, when both
	// marshalling and unmarshalling, we have to consult the extension
	// type list in the plan to obtain keys. Look at p.Yaml(), we
	// cannot easily related sections in the combined layer to the keys.

	// REVIEW NOTE #6: Would it not be great if we can ask a layer to
	// marshal or unmarshal itself. Currently we have to involve the plan,
	// because even if we return the Combined layer, it cannot do it.

	// Layer extensions
	Sections map[LayerSection]LayerSection `yaml:"-"`
}

type Service struct {
	// Basic details
	Name        string         `yaml:"-"`
	Summary     string         `yaml:"summary,omitempty"`
	Description string         `yaml:"description,omitempty"`
	Startup     ServiceStartup `yaml:"startup,omitempty"`
	Override    Override       `yaml:"override,omitempty"`
	Command     string         `yaml:"command,omitempty"`

	// Service dependencies
	After    []string `yaml:"after,omitempty"`
	Before   []string `yaml:"before,omitempty"`
	Requires []string `yaml:"requires,omitempty"`

	// Options for command execution
	Environment map[string]string `yaml:"environment,omitempty"`
	UserID      *int              `yaml:"user-id,omitempty"`
	User        string            `yaml:"user,omitempty"`
	GroupID     *int              `yaml:"group-id,omitempty"`
	Group       string            `yaml:"group,omitempty"`
	WorkingDir  string            `yaml:"working-dir,omitempty"`

	// Auto-restart and backoff functionality
	OnSuccess      ServiceAction            `yaml:"on-success,omitempty"`
	OnFailure      ServiceAction            `yaml:"on-failure,omitempty"`
	OnCheckFailure map[string]ServiceAction `yaml:"on-check-failure,omitempty"`
	BackoffDelay   OptionalDuration         `yaml:"backoff-delay,omitempty"`
	BackoffFactor  OptionalFloat            `yaml:"backoff-factor,omitempty"`
	BackoffLimit   OptionalDuration         `yaml:"backoff-limit,omitempty"`
	KillDelay      OptionalDuration         `yaml:"kill-delay,omitempty"`
}

// Copy returns a deep copy of the service.
func (s *Service) Copy() *Service {
	copied := *s
	copied.After = append([]string(nil), s.After...)
	copied.Before = append([]string(nil), s.Before...)
	copied.Requires = append([]string(nil), s.Requires...)
	if s.Environment != nil {
		copied.Environment = make(map[string]string)
		for k, v := range s.Environment {
			copied.Environment[k] = v
		}
	}
	if s.UserID != nil {
		copied.UserID = copyIntPtr(s.UserID)
	}
	if s.GroupID != nil {
		copied.GroupID = copyIntPtr(s.GroupID)
	}
	if s.OnCheckFailure != nil {
		copied.OnCheckFailure = make(map[string]ServiceAction)
		for k, v := range s.OnCheckFailure {
			copied.OnCheckFailure[k] = v
		}
	}
	return &copied
}

// Merge merges the fields set in other into s.
func (s *Service) Merge(other *Service) {
	if other.Summary != "" {
		s.Summary = other.Summary
	}
	if other.Description != "" {
		s.Description = other.Description
	}
	if other.Startup != StartupUnknown {
		s.Startup = other.Startup
	}
	if other.Command != "" {
		s.Command = other.Command
	}
	if other.KillDelay.IsSet {
		s.KillDelay = other.KillDelay
	}
	if other.UserID != nil {
		s.UserID = copyIntPtr(other.UserID)
	}
	if other.User != "" {
		s.User = other.User
	}
	if other.GroupID != nil {
		s.GroupID = copyIntPtr(other.GroupID)
	}
	if other.Group != "" {
		s.Group = other.Group
	}
	if other.WorkingDir != "" {
		s.WorkingDir = other.WorkingDir
	}
	s.After = append(s.After, other.After...)
	s.Before = append(s.Before, other.Before...)
	s.Requires = append(s.Requires, other.Requires...)
	for k, v := range other.Environment {
		if s.Environment == nil {
			s.Environment = make(map[string]string)
		}
		s.Environment[k] = v
	}
	if other.OnSuccess != "" {
		s.OnSuccess = other.OnSuccess
	}
	if other.OnFailure != "" {
		s.OnFailure = other.OnFailure
	}
	for k, v := range other.OnCheckFailure {
		if s.OnCheckFailure == nil {
			s.OnCheckFailure = make(map[string]ServiceAction)
		}
		s.OnCheckFailure[k] = v
	}
	if other.BackoffDelay.IsSet {
		s.BackoffDelay = other.BackoffDelay
	}
	if other.BackoffFactor.IsSet {
		s.BackoffFactor = other.BackoffFactor
	}
	if other.BackoffLimit.IsSet {
		s.BackoffLimit = other.BackoffLimit
	}
}

// Equal returns true when the two services are equal in value.
func (s *Service) Equal(other *Service) bool {
	if s == other {
		return true
	}
	return reflect.DeepEqual(s, other)
}

// ParseCommand returns a service command as two stream of strings.
// The base command is returned as a stream and the default arguments
// in [ ... ] group is returned as another stream.
func (s *Service) ParseCommand() (base, extra []string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("cannot parse service %q command: %w", s.Name, err)
		}
	}()

	args, err := shlex.Split(s.Command)
	if err != nil {
		return nil, nil, err
	}

	var inBrackets, gotBrackets bool

	for idx, arg := range args {
		if inBrackets {
			if arg == "[" {
				return nil, nil, fmt.Errorf("cannot nest [ ... ] groups")
			}
			if arg == "]" {
				inBrackets = false
				continue
			}
			extra = append(extra, arg)
			continue
		}
		if gotBrackets {
			return nil, nil, fmt.Errorf("cannot have any arguments after [ ... ] group")
		}
		if arg == "[" {
			if idx == 0 {
				return nil, nil, fmt.Errorf("cannot start command with [ ... ] group")
			}
			inBrackets = true
			gotBrackets = true
			continue
		}
		if arg == "]" {
			return nil, nil, fmt.Errorf("cannot have ] outside of [ ... ] group")
		}
		base = append(base, arg)
	}

	return base, extra, nil
}

// CommandString returns a service command as a string after
// appending the arguments in "extra" to the command in "base"
func CommandString(base, extra []string) string {
	output := shlex.Join(base)
	if len(extra) > 0 {
		output = output + " [ " + shlex.Join(extra) + " ]"
	}
	return output
}

// LogsTo returns true if the logs from s should be forwarded to target t.
func (s *Service) LogsTo(t *LogTarget) bool {
	// Iterate backwards through t.Services until we find something matching
	// s.Name.
	for i := len(t.Services) - 1; i >= 0; i-- {
		switch t.Services[i] {
		case s.Name:
			return true
		case ("-" + s.Name):
			return false
		case "all":
			return true
		case "-all":
			return false
		}
	}
	// Nothing matching the service name, so it was not specified.
	return false
}

type ServiceStartup string

const (
	StartupUnknown  ServiceStartup = ""
	StartupEnabled  ServiceStartup = "enabled"
	StartupDisabled ServiceStartup = "disabled"
)

// Override specifies the layer override mechanism for an object.
type Override string

const (
	UnknownOverride Override = ""
	MergeOverride   Override = "merge"
	ReplaceOverride Override = "replace"
)

type ServiceAction string

const (
	// Actions allowed in all contexts
	ActionUnset    ServiceAction = ""
	ActionRestart  ServiceAction = "restart"
	ActionShutdown ServiceAction = "shutdown"
	ActionIgnore   ServiceAction = "ignore"

	// Actions only allowed in specific contexts
	ActionFailureShutdown ServiceAction = "failure-shutdown"
	ActionSuccessShutdown ServiceAction = "success-shutdown"
)

// Check specifies configuration for a single health check.
type Check struct {
	// Basic details
	Name     string     `yaml:"-"`
	Override Override   `yaml:"override,omitempty"`
	Level    CheckLevel `yaml:"level,omitempty"`

	// Common check settings
	Period    OptionalDuration `yaml:"period,omitempty"`
	Timeout   OptionalDuration `yaml:"timeout,omitempty"`
	Threshold int              `yaml:"threshold,omitempty"`

	// Type-specific check settings (only one of these can be set)
	HTTP *HTTPCheck `yaml:"http,omitempty"`
	TCP  *TCPCheck  `yaml:"tcp,omitempty"`
	Exec *ExecCheck `yaml:"exec,omitempty"`
}

// Copy returns a deep copy of the check configuration.
func (c *Check) Copy() *Check {
	copied := *c
	if c.HTTP != nil {
		copied.HTTP = c.HTTP.Copy()
	}
	if c.TCP != nil {
		copied.TCP = c.TCP.Copy()
	}
	if c.Exec != nil {
		copied.Exec = c.Exec.Copy()
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *Check) Merge(other *Check) {
	if other.Level != "" {
		c.Level = other.Level
	}
	if other.Period.IsSet {
		c.Period = other.Period
	}
	if other.Timeout.IsSet {
		c.Timeout = other.Timeout
	}
	if other.Threshold != 0 {
		c.Threshold = other.Threshold
	}
	if other.HTTP != nil {
		if c.HTTP == nil {
			c.HTTP = &HTTPCheck{}
		}
		c.HTTP.Merge(other.HTTP)
	}
	if other.TCP != nil {
		if c.TCP == nil {
			c.TCP = &TCPCheck{}
		}
		c.TCP.Merge(other.TCP)
	}
	if other.Exec != nil {
		if c.Exec == nil {
			c.Exec = &ExecCheck{}
		}
		c.Exec.Merge(other.Exec)
	}
}

// CheckLevel specifies the optional check level.
type CheckLevel string

const (
	UnsetLevel CheckLevel = ""
	AliveLevel CheckLevel = "alive"
	ReadyLevel CheckLevel = "ready"
)

// HTTPCheck holds the configuration for an HTTP health check.
type HTTPCheck struct {
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// Copy returns a deep copy of the HTTP check configuration.
func (c *HTTPCheck) Copy() *HTTPCheck {
	copied := *c
	if c.Headers != nil {
		copied.Headers = make(map[string]string, len(c.Headers))
		for k, v := range c.Headers {
			copied.Headers[k] = v
		}
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *HTTPCheck) Merge(other *HTTPCheck) {
	if other.URL != "" {
		c.URL = other.URL
	}
	for k, v := range other.Headers {
		if c.Headers == nil {
			c.Headers = make(map[string]string)
		}
		c.Headers[k] = v
	}
}

// TCPCheck holds the configuration for an HTTP health check.
type TCPCheck struct {
	Port int    `yaml:"port,omitempty"`
	Host string `yaml:"host,omitempty"`
}

// Copy returns a deep copy of the TCP check configuration.
func (c *TCPCheck) Copy() *TCPCheck {
	copied := *c
	return &copied
}

// Merge merges the fields set in other into c.
func (c *TCPCheck) Merge(other *TCPCheck) {
	if other.Port != 0 {
		c.Port = other.Port
	}
	if other.Host != "" {
		c.Host = other.Host
	}
}

// ExecCheck holds the configuration for an exec health check.
type ExecCheck struct {
	Command        string            `yaml:"command,omitempty"`
	ServiceContext string            `yaml:"service-context,omitempty"`
	Environment    map[string]string `yaml:"environment,omitempty"`
	UserID         *int              `yaml:"user-id,omitempty"`
	User           string            `yaml:"user,omitempty"`
	GroupID        *int              `yaml:"group-id,omitempty"`
	Group          string            `yaml:"group,omitempty"`
	WorkingDir     string            `yaml:"working-dir,omitempty"`
}

// Copy returns a deep copy of the exec check configuration.
func (c *ExecCheck) Copy() *ExecCheck {
	copied := *c
	if c.Environment != nil {
		copied.Environment = make(map[string]string, len(c.Environment))
		for k, v := range c.Environment {
			copied.Environment[k] = v
		}
	}
	if c.UserID != nil {
		copied.UserID = copyIntPtr(c.UserID)
	}
	if c.GroupID != nil {
		copied.GroupID = copyIntPtr(c.GroupID)
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *ExecCheck) Merge(other *ExecCheck) {
	if other.Command != "" {
		c.Command = other.Command
	}
	if other.ServiceContext != "" {
		c.ServiceContext = other.ServiceContext
	}
	for k, v := range other.Environment {
		if c.Environment == nil {
			c.Environment = make(map[string]string)
		}
		c.Environment[k] = v
	}
	if other.UserID != nil {
		c.UserID = copyIntPtr(other.UserID)
	}
	if other.User != "" {
		c.User = other.User
	}
	if other.GroupID != nil {
		c.GroupID = copyIntPtr(other.GroupID)
	}
	if other.Group != "" {
		c.Group = other.Group
	}
	if other.WorkingDir != "" {
		c.WorkingDir = other.WorkingDir
	}
}

// LogTarget specifies a remote server to forward logs to.
type LogTarget struct {
	Name     string            `yaml:"-"`
	Type     LogTargetType     `yaml:"type"`
	Location string            `yaml:"location"`
	Services []string          `yaml:"services"`
	Override Override          `yaml:"override,omitempty"`
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
// Neither the individual layers nor the combined layer are validated here - the
// caller should have validated the individual layers prior to calling, and
// validate the combined output if required.
func (p *Plan) CombineLayers(layers ...*Layer) (*Layer, error) {
	combined, err := p.newLayer()
	if err != nil {
		return nil, err
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

		// Combine the sections
		for key, _ := range combined.Sections {
			err := combined.Sections[key].Combine(layer.Sections[key])
			if err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf(`cannot combine section from layer %q: %v`, layer.Label, err),
				}
			}
		}
	}

	// Set defaults where required.
	for _, service := range combined.Services {
		if !service.BackoffDelay.IsSet {
			service.BackoffDelay.Value = defaultBackoffDelay
		}
		if !service.BackoffFactor.IsSet {
			service.BackoffFactor.Value = defaultBackoffFactor
		}
		if !service.BackoffLimit.IsSet {
			service.BackoffLimit.Value = defaultBackoffLimit
		}
	}

	for _, check := range combined.Checks {
		if !check.Period.IsSet {
			check.Period.Value = defaultCheckPeriod
		}
		if !check.Timeout.IsSet {
			check.Timeout.Value = defaultCheckTimeout
		}
		if check.Timeout.Value > check.Period.Value {
			// The effective timeout will be the period, so make that clear.
			// `.IsSet` remains false so that the capped value does not appear
			// in the combined plan output - and it's not *user* set - the
			// effective default timeout is the minimum of (check.Period.Value,
			// default timeout).
			check.Timeout.Value = check.Period.Value
		}
		if check.Threshold == 0 {
			// Default number of failures in a row before check triggers
			// action, default is >1 to avoid flapping due to glitches. For
			// what it's worth, Kubernetes probes uses a default of 3 too.
			check.Threshold = defaultCheckThreshold
		}
	}

	return combined, nil
}

// AddSection adds a new section to the layer section map.
func (layer *Layer) AddSection(section LayerSection) error {
	ptrv := reflect.ValueOf(section)
	if ptrv.Kind() != reflect.Pointer {
		return fmt.Errorf("internal error: need a pointer to layer section type")
	}
	// We use an interface (layer section type, nil value) as the map key.
	key := reflect.Zero(ptrv.Type()).Interface().(LayerSection)
	layer.Sections[key] = section
	return nil
}

// Section retrieves a layer section from a layer. The argument must be a
// pointer to a LayerSection compatible type address, to allow the method
// to update the provided nil pointer itself.
func (layer *Layer) Section(section interface{}) error {
	vPtrPtr := reflect.ValueOf(section)
	if vPtrPtr.Kind() != reflect.Pointer && vPtrPtr.Elem().Kind() != reflect.Pointer {
		return fmt.Errorf("internal error: need a pointer to pointer to type")
	}
	// Is the provided interface compatible (we allow any to be passed in)
	vPtr := vPtrPtr.Elem()
	key, ok := vPtr.Interface().(LayerSection)
	if !ok {
		return fmt.Errorf(
			"internal error: argument not a valid layer section interface")
	}
	// We expect the pointer to be nil, so we can update it to point to the
	// retrieved layer section object.
	if !vPtr.IsZero() {
		return fmt.Errorf(
			"internal error: layer section pointer must be nil")
	}
	// We use an interface (layer section type, nil value) as the map key.
	ls := layer.Sections[key]
	vPtr.Set(reflect.ValueOf(ls))
	return nil
}

// Validate checks that the layer is valid. It returns nil if all the checks pass, or
// an error if there are validation errors.
// See also Plan.Validate, which does additional checks based on the combined
// layers.
func (layer *Layer) Validate() error {
	for name, service := range layer.Services {
		if name == "" {
			return &FormatError{
				Message: fmt.Sprintf("cannot use empty string as service name"),
			}
		}
		if name == "pebble" {
			// Disallow service name "pebble" to avoid ambiguity (for example,
			// in log output).
			return &FormatError{
				Message: fmt.Sprintf("cannot use reserved service name %q", name),
			}
		}
		// Deprecated service names
		if name == "all" || name == "default" || name == "none" {
			logger.Noticef("Using keyword %q as a service name is deprecated", name)
		}
		if strings.HasPrefix(name, "-") {
			return &FormatError{
				Message: fmt.Sprintf(`cannot use service name %q: starting with "-" not allowed`, name),
			}
		}
		if service == nil {
			return &FormatError{
				Message: fmt.Sprintf("service object cannot be null for service %q", name),
			}
		}
		_, _, err := service.ParseCommand()
		if err != nil {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q command invalid: %v", name, err),
			}
		}
		if !validServiceAction(service.OnSuccess, ActionFailureShutdown) {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q on-success action %q invalid", name, service.OnSuccess),
			}
		}
		if !validServiceAction(service.OnFailure, ActionSuccessShutdown) {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q on-failure action %q invalid", name, service.OnFailure),
			}
		}
		for _, action := range service.OnCheckFailure {
			if !validServiceAction(action, ActionSuccessShutdown) {
				return &FormatError{
					Message: fmt.Sprintf("plan service %q on-check-failure action %q invalid", name, action),
				}
			}
		}
		if service.BackoffFactor.IsSet && service.BackoffFactor.Value < 1 {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q backoff-factor must be 1.0 or greater, not %g", name, service.BackoffFactor.Value),
			}
		}
	}

	for name, check := range layer.Checks {
		if name == "" {
			return &FormatError{
				Message: fmt.Sprintf("cannot use empty string as check name"),
			}
		}
		if check == nil {
			return &FormatError{
				Message: fmt.Sprintf("check object cannot be null for check %q", name),
			}
		}
		if name == "" {
			return &FormatError{
				Message: fmt.Sprintf("cannot use empty string as log target name"),
			}
		}
		if check.Level != UnsetLevel && check.Level != AliveLevel && check.Level != ReadyLevel {
			return &FormatError{
				Message: fmt.Sprintf(`plan check %q level must be "alive" or "ready"`, name),
			}
		}
		if check.Period.IsSet && check.Period.Value == 0 {
			return &FormatError{
				Message: fmt.Sprintf("plan check %q period must not be zero", name),
			}
		}
		if check.Timeout.IsSet && check.Timeout.Value == 0 {
			return &FormatError{
				Message: fmt.Sprintf("plan check %q timeout must not be zero", name),
			}
		}

		if check.Exec != nil {
			_, err := shlex.Split(check.Exec.Command)
			if err != nil {
				return &FormatError{
					Message: fmt.Sprintf("plan check %q command invalid: %v", name, err),
				}
			}
			_, _, err = osutil.NormalizeUidGid(check.Exec.UserID, check.Exec.GroupID, check.Exec.User, check.Exec.Group)
			if err != nil {
				return &FormatError{
					Message: fmt.Sprintf("plan check %q has invalid user/group: %v", name, err),
				}
			}
		}
	}

	for name, target := range layer.LogTargets {
		if target == nil {
			return &FormatError{
				Message: fmt.Sprintf("log target object cannot be null for log target %q", name),
			}
		}
		for labelName := range target.Labels {
			// 'pebble_*' labels are reserved
			if strings.HasPrefix(labelName, "pebble_") {
				return &FormatError{
					Message: fmt.Sprintf(`log target %q: label %q uses reserved prefix "pebble_"`, name, labelName),
				}
			}
		}
		switch target.Type {
		case LokiTarget, SyslogTarget:
			// valid, continue
		case UnsetLogTarget:
			// will be checked when the layers are combined
		default:
			return &FormatError{
				Message: fmt.Sprintf(`log target %q has unsupported type %q, must be %q or %q`,
					name, target.Type, LokiTarget, SyslogTarget),
			}
		}
	}

	return nil
}

// Validate checks that the combined layers form a valid plan.
// See also Layer.Validate, which checks that the individual layers are valid.
func (p *Plan) Validate() error {
	for name, service := range p.Combined.Services {
		if service.Command == "" {
			return &FormatError{
				Message: fmt.Sprintf(`plan must define "command" for service %q`, name),
			}
		}
	}

	for name, check := range p.Combined.Checks {
		numTypes := 0
		if check.HTTP != nil {
			if check.HTTP.URL == "" {
				return &FormatError{
					Message: fmt.Sprintf(`plan must set "url" for http check %q`, name),
				}
			}
			numTypes++
		}
		if check.TCP != nil {
			if check.TCP.Port == 0 {
				return &FormatError{
					Message: fmt.Sprintf(`plan must set "port" for tcp check %q`, name),
				}
			}
			numTypes++
		}
		if check.Exec != nil {
			if check.Exec.Command == "" {
				return &FormatError{
					Message: fmt.Sprintf(`plan must set "command" for exec check %q`, name),
				}
			}
			_, contextExists := p.Combined.Services[check.Exec.ServiceContext]
			if check.Exec.ServiceContext != "" && !contextExists {
				return &FormatError{
					Message: fmt.Sprintf("plan check %q service context specifies non-existent service %q",
						name, check.Exec.ServiceContext),
				}
			}
			numTypes++
		}
		if numTypes != 1 {
			return &FormatError{
				Message: fmt.Sprintf(`plan must specify one of "http", "tcp", or "exec" for check %q`, name),
			}
		}
	}

	for name, target := range p.Combined.LogTargets {
		switch target.Type {
		case LokiTarget, SyslogTarget:
			// valid, continue
		case UnsetLogTarget:
			return &FormatError{
				Message: fmt.Sprintf(`plan must define "type" (%q or %q) for log target %q`,
					LokiTarget, SyslogTarget, name),
			}
		}

		// Validate service names specified in log target.
		for _, serviceName := range target.Services {
			serviceName = strings.TrimPrefix(serviceName, "-")
			if serviceName == "all" {
				continue
			}
			if _, ok := p.Combined.Services[serviceName]; ok {
				continue
			}
			return &FormatError{
				Message: fmt.Sprintf(`log target %q specifies unknown service %q`,
					target.Name, serviceName),
			}
		}

		if target.Location == "" {
			return &FormatError{
				Message: fmt.Sprintf(`plan must define "location" for log target %q`, name),
			}
		}
	}

	// Ensure combined layers don't have cycles.
	err := p.checkCycles()
	if err != nil {
		return err
	}

	// Ask the sections to verify the combined plan is valid.
	for _, section := range p.Combined.Sections {
		err := section.Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

// StartOrder returns the required services that must be started for the named
// services to be properly started, in the order that they must be started.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StartOrder(names []string) ([]string, error) {
	return order(p.Combined.Services, names, false)
}

// StopOrder returns the required services that must be stopped for the named
// services to be properly stopped, in the order that they must be stopped.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StopOrder(names []string) ([]string, error) {
	return order(p.Combined.Services, names, true)
}

func order(services map[string]*Service, names []string, stop bool) ([]string, error) {
	// For stop, create a list of reversed dependencies.
	predecessors := map[string][]string(nil)
	if stop {
		predecessors = make(map[string][]string)
		for name, service := range services {
			for _, req := range service.Requires {
				predecessors[req] = append(predecessors[req], name)
			}
		}
	}

	// Collect all services that will be started or stopped.
	successors := map[string][]string{}
	pending := append([]string(nil), names...)
	for i := 0; i < len(pending); i++ {
		name := pending[i]
		if _, seen := successors[name]; seen {
			continue
		}
		successors[name] = nil
		if stop {
			pending = append(pending, predecessors[name]...)
		} else {
			service, ok := services[name]
			if !ok {
				return nil, &FormatError{
					Message: fmt.Sprintf("service %q does not exist", name),
				}
			}
			pending = append(pending, service.Requires...)
		}
	}

	// Create a list of successors involving those services only.
	for name := range successors {
		service, ok := services[name]
		if !ok {
			return nil, &FormatError{
				Message: fmt.Sprintf("service %q does not exist", name),
			}
		}
		succs := successors[name]
		serviceAfter := service.After
		serviceBefore := service.Before
		if stop {
			serviceAfter, serviceBefore = serviceBefore, serviceAfter
		}
		for _, after := range serviceAfter {
			if _, required := successors[after]; required {
				succs = append(succs, after)
			}
		}
		successors[name] = succs
		for _, before := range serviceBefore {
			if succs, required := successors[before]; required {
				successors[before] = append(succs, name)
			}
		}
	}

	// Sort them up.
	var order []string
	for _, names := range tarjanSort(successors) {
		if len(names) > 1 {
			return nil, &FormatError{
				Message: fmt.Sprintf("services in before/after loop: %s", strings.Join(names, ", ")),
			}
		}
		order = append(order, names[0])
	}
	return order, nil
}

func (p *Plan) checkCycles() error {
	var names []string
	for name := range p.Combined.Services {
		names = append(names, name)
	}
	_, err := order(p.Combined.Services, names, false)
	return err
}

// newLayer creates an empty layer with all the layer section extensions
// added. We always add all layer sections, although we could not know
// before if all of them will be used in any specific layer. This allows
// for simpler code when marshalling and unmarshalling.
func (p *Plan) newLayer() (layer *Layer, err error) {
	layer = &Layer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
		Sections:   make(map[LayerSection]LayerSection),
	}
	for key, ext := range p.layerExtensions {
		err := layer.AddSection(ext.EmptySection(layer))
		if err != nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot add layer section %q: %v", key, err),
			}
		}
	}
	return layer, nil
}

func (p *Plan) ParseLayer(order int, label string, data []byte) (*Layer, error) {
	var planOutline map[string]yaml.Node
	layer, err := p.newLayer()
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(data, &planOutline)
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}
	statics := map[string]interface{}{
		"summary":     &layer.Summary,
		"description": &layer.Description,
		"services":    &layer.Services,
		"checks":      &layer.Checks,
		"log-targets": &layer.LogTargets,
	}
	for k, v := range planOutline {
		switch k {
		case "services", "checks", "log-targets", "summary", "description":
			if err = v.Decode(statics[k]); err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
				}
			}
		default:
			if ext, ok := p.layerExtensions[k]; ok {
				section, err := ext.ParseSection(layer, &v)
				if err != nil {
					return nil, &FormatError{
						Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
					}
				}
				err = layer.AddSection(section)
				if err != nil {
					return nil, &FormatError{
						Message: fmt.Sprintf("cannot add layer section %q: %v", label, err),
					}
				}
			} else {
				return nil, &FormatError{
					Message: fmt.Sprintf("cannot parse layer %q: unknown section %q", label, k),
				}
			}
		}
	}
	layer.Order = order
	layer.Label = label

	for name, service := range layer.Services {
		// If service is nil, then the validation below will reject the layer,
		// but we want the name set so that we can use easily use it in error
		// messages during validation.
		if service != nil {
			service.Name = name
		}
	}
	for name, check := range layer.Checks {
		if check != nil {
			check.Name = name
		}
	}
	for name, target := range layer.LogTargets {
		if target != nil {
			target.Name = name
		}
	}

	err = layer.Validate()
	if err != nil {
		return nil, err
	}

	return layer, err
}

func validServiceAction(action ServiceAction, additionalValid ...ServiceAction) bool {
	for _, v := range additionalValid {
		if action == v {
			return true
		}
	}
	switch action {
	case ActionUnset, ActionRestart, ActionShutdown, ActionIgnore:
		return true
	default:
		return false
	}
}

var fnameExp = regexp.MustCompile("^([0-9]{3})-([a-z](?:-?[a-z0-9]){2,}).yaml$")

func (p *Plan) readLayersDir(dirname string) ([]*Layer, error) {
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

		layer, err := p.ParseLayer(order, label, data)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// Load reads the configuration layers from the "layers" sub-directory in
// dir, and overwrites the plan. If the "layers" sub-directory doesn't
// exist, it returns a valid Plan with no layers.
func (p *Plan) Load(dir string) error {
	layersDir := filepath.Join(dir, "layers")
	_, err := os.Stat(layersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	layers, err := p.readLayersDir(layersDir)
	if err != nil {
		return err
	}
	combined, err := p.CombineLayers(layers...)
	if err != nil {
		return err
	}

	// Replace the plan
	p.Layers = layers
	p.Combined = combined

	// TODO: Maybe Validate the combined layer before updating the plan
	err = p.Validate()
	if err != nil {
		return err
	}
	return err
}

// MergeServiceContext merges the overrides on top of the service context
// specified by serviceName, returning a new ContextOptions value. If
// serviceName is "" (context not specified), return overrides directly.
func (p *Plan) MergeServiceContext(serviceName string, overrides ContextOptions) (ContextOptions, error) {
	if serviceName == "" {
		return overrides, nil
	}
	var service *Service
	if p.Combined != nil {
		for _, s := range p.Combined.Services {
			if s.Name == serviceName {
				service = s
				break
			}
		}
	}
	if service == nil {
		return ContextOptions{}, fmt.Errorf("context service %q not found", serviceName)
	}

	// Start with the config values from the context service.
	merged := ContextOptions{
		Environment: make(map[string]string),
	}
	for k, v := range service.Environment {
		merged.Environment[k] = v
	}
	if service.UserID != nil {
		merged.UserID = copyIntPtr(service.UserID)
	}
	merged.User = service.User
	if service.GroupID != nil {
		merged.GroupID = copyIntPtr(service.GroupID)
	}
	merged.Group = service.Group
	merged.WorkingDir = service.WorkingDir

	// Merge in fields from the overrides, if set.
	for k, v := range overrides.Environment {
		merged.Environment[k] = v
	}
	if overrides.UserID != nil {
		merged.UserID = copyIntPtr(overrides.UserID)
	}
	if overrides.User != "" {
		merged.User = overrides.User
	}
	if overrides.GroupID != nil {
		merged.GroupID = copyIntPtr(overrides.GroupID)
	}
	if overrides.Group != "" {
		merged.Group = overrides.Group
	}
	if overrides.WorkingDir != "" {
		merged.WorkingDir = overrides.WorkingDir
	}

	return merged, nil
}

// ContextOptions holds service context config fields.
type ContextOptions struct {
	Environment map[string]string
	UserID      *int
	User        string
	GroupID     *int
	Group       string
	WorkingDir  string
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	copied := *p
	return &copied
}
