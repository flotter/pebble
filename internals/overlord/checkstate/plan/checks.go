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
