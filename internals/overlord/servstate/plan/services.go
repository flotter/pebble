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

const (
	defaultBackoffDelay  = 500 * time.Millisecond
	defaultBackoffFactor = 2.0
	defaultBackoffLimit  = 30 * time.Second
)

type ServiceStartup string

const (
	StartupUnknown  ServiceStartup = ""
	StartupEnabled  ServiceStartup = "enabled"
	StartupDisabled ServiceStartup = "disabled"
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

// StartOrder returns the required services that must be started for the named
// services to be properly started, in the order that they must be started.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StartOrder(names []string) ([]string, error) {
	return order(p.Services, names, false)
}

// StopOrder returns the required services that must be stopped for the named
// services to be properly stopped, in the order that they must be stopped.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StopOrder(names []string) ([]string, error) {
	return order(p.Services, names, true)
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

// MergeServiceContext merges the overrides on top of the service context
// specified by serviceName, returning a new ContextOptions value. If
// serviceName is "" (context not specified), return overrides directly.
func MergeServiceContext(p *Plan, serviceName string, overrides ContextOptions) (ContextOptions, error) {
	if serviceName == "" {
		return overrides, nil
	}
	var service *Service
	for _, s := range p.Services {
		if s.Name == serviceName {
			service = s
			break
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

/////////////// -------------



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


	// Ensure combined layers don't have cycles.
	err := combined.checkCycles()
	if err != nil {
		return nil, err
	}


func (l *Layer) checkCycles() error {
	var names []string
	for name := range l.Services {
		names = append(names, name)
	}
	_, err := order(l.Services, names, false)
	return err
}

////---- parselayer
	
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



	err = layer.checkCycles()
	if err != nil {
		return nil, err
	}
