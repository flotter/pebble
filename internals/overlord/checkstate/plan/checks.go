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
	defaultCheckPeriod    = 10 * time.Second
	defaultCheckTimeout   = 3 * time.Second
	defaultCheckThreshold = 3
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

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	copied := *p
	return &copied
}

///////// -----------------


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

// --- parselayer
	

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
