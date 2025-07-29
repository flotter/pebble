// Copyright (c) 2024 Canonical Ltd
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

// This package implements the Netplan v2 schema Plan extension.
package pairingstate

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
)

var _ plan.SectionExtension = (*SectionExtension)(nil)

// SectionExtension implements the Pebble plan.SectionExtension interface.
type SectionExtension struct{}

// NewSectionExtension creates a new Pairing Manager extension for the plan library.
func NewSectionExtension() (*SectionExtension, error) {
	return &SectionExtension{}, nil
}

func (s SectionExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	inspectConfig := struct {
		Override   plan.Override  `yaml:"override,omitempty"`
		Config     ManagerConfig `yaml:",inline"`
		Controller yaml.Node `yaml:"controller"`
	}{}
	// The following issue prevents us from using the yaml.Node decoder
	// with KnownFields = true behaviour. Once one of the proposals get
	// merged, we can remove the intermediate Marshall step.
	// https://github.com/go-yaml/yaml/issues/460
	if len(data.Content) != 0 {
		yml, err := yaml.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot marshal pairingstate section: %w", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(yml))
		dec.KnownFields(true)
		if err = dec.Decode(&inspectConfig); err != nil {
			return nil, &plan.FormatError{
				Message: fmt.Sprintf("cannot parse the pairingstate section: %v", err),
			}
		}
	} else {
		// If the layer is empty, we auto-insert a merge override
		// option by default. Without this, we force the plan
		// layer to explicitly provide the override, even if no
		// pairing key exists in it.
		inspectConfig.Override = plan.MergeOverride
	}

	// We no longer want the "type" include in the YAML data. This will
	// allow the "controller" to only contain the controller specific
	// fields, which we can now safely unmarshall with KnownFields to
	// detect unsupported fields getting passed to the controller.
	controllerType, controllerConfigSupplied, err := removeControllerType(&inspectConfig.Controller)
	if err != nil {
		return nil, err
	}

	if controllerConfigSupplied && controllerType == "" {
		return nil, fmt.Errorf("cannot decode pairing controller configuration: type missing")
	}

	// If a type was supplied, locate the controller extension and
	// unmarshall the controller configuration.
	var controllerExt ControllerExtension
	if controllerType != "" {
		var ok bool
		controllerExt, ok = controllerExtensions[controllerType]
		if !ok {
			return nil, fmt.Errorf("cannot decode pairing controller configuration: unknown type %q", controllerType)
		}
	}

	// Now create the proper pairing configuration.
	newSectionLayer := &PairingConfig{}
	newSectionLayer.Override = inspectConfig.Override
	newSectionLayer.Config = inspectConfig.Config

	if controllerExt != nil {
		controllerCfg, err := controllerExt.ParseConfig(inspectConfig.Controller)
		if err != nil {
			return nil, err
		}
		newSectionLayer.Controller.Type = controllerType
		newSectionLayer.Controller.Config = controllerCfg
	}
	return newSectionLayer, nil
}

func (s SectionExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	combinedSection := &PairingConfig{}

	// Combine the manager comnfigurations.
	for _, section := range sections {
		pairingLayer, ok := section.(*PairingConfig)
		if !ok {
			return nil, fmt.Errorf("internal error: invalid section type %T", pairingLayer)
		}
		err := combinedSection.combineManagerConfig(pairingLayer)
		if err != nil {
			return nil, err
		}
	}

	// Apply default pairing mode.
	if combinedSection.Config.Mode == "" {
		combinedSection.Config.Mode = modeDisabled
	}

	// Combine the controller configurations.
	controllerType, controllerConfigs, err := checkControllers(sections)
	if err != nil {
		return nil, err
	}
	if controllerType != "" {
		combinedSection.Controller.Type = controllerType
		controllerExt, ok := controllerExtensions[controllerType]
		if !ok {
			return nil, fmt.Errorf("cannot decode pairing controller configuration: unknown type %q", controllerType)
		}

		combinedSection.Controller.Config, err = controllerExt.CombineConfigs(controllerConfigs...)
		if err != nil {
			return nil, err
		}
	}

	return combinedSection, nil
}

// checkControllers checks if it is possible to combine the pairing controller
// configs. Merging is only possible between two layers if controllers are the
// same type, or if one of the controllers are not supplied (empty type). The
// function returns a slice of pairing controller configurations that needs
// merging (with the replaced configs removed).
func checkControllers(sections []plan.Section) (string, []ControllerConfig, error) {
	controllerType := ""
	mergeStart := 0
	for i, section := range sections {
		pairingLayer, ok := section.(*PairingConfig)
		if !ok {
			return "", nil, fmt.Errorf("internal error: invalid section type %T", pairingLayer)
		}
		// If the controller changes between layers, the upper layer
		// must request a replace.
		if controllerType != "" &&
			pairingLayer.Controller.Type != "" &&
			controllerType != pairingLayer.Controller.Type {

			if pairingLayer.Override != plan.ReplaceOverride {
				return "", nil, errors.New("cannot merge different controller configurations (only replace)")
			}

			// The pairing controller configurations before this
			// point can safely be ignored as thet are getting
			// replaced.
			mergeStart = i
		}

		if pairingLayer.Controller.Type != "" {
			controllerType = pairingLayer.Controller.Type
		}
	}
	// Extract only the controller configurations.
	var cfgs []ControllerConfig
	for _, section := range sections[mergeStart:] {
		pairingLayer := section.(*PairingConfig)
		cfgs = append(cfgs, pairingLayer.Controller.Config)
	}
	return controllerType, cfgs, nil
}

// ValidatePlan is only called once the final plan is produced, based on
// all the layers merged.
func (s SectionExtension) ValidatePlan(p *plan.Plan) error {
	// We have no cross-section validation to do in this case.
	return nil
}

// PairingField is the top level string key used in the Pebble plan.
const PairingField string = "pairing"

// PairingConfig represents a config that includes both the pairing manager
// and pairing controller configuration.
type PairingConfig struct {
	Override   plan.Override  `yaml:"override,omitempty"`
	Config     ManagerConfig `yaml:",inline"`
	Controller PairingController `yaml:"controller,omitempty"`
}

func (c *PairingConfig) IsZero() bool {
	// Let's always marshall at least the mode, so that even if no pairing
	// configuration is explicitly specified, it would marshall as pairing
	// mode disabled.
	return false
}

func (c *PairingConfig) Validate() error {
	if err := c.Config.validate(); err != nil {
		return err
	}
	// Only validate a controller if a type was supplied.
	if c.Controller.Config != nil {
		if err := c.Controller.Config.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *PairingConfig) combineManagerConfig(other *PairingConfig) error {
	switch other.Override {
	case plan.MergeOverride:
		c.Config.merge(other.Config)
	case plan.ReplaceOverride:
		c.Config = other.Config.copy()
	case plan.UnknownOverride:
		return &plan.FormatError{
			Message: `pairing must define an "override" policy`,
		}
	default:
		return &plan.FormatError{
			Message: fmt.Sprintf(`pairing has an invalid "override" policy: %q`, c.Override),
		}
	}
	return nil
}

type PairingController struct {
	Type   string           `yaml:"type"`
	Config ControllerConfig `yaml:"config"`
}

func (p *PairingController) MarshalYAML() (interface{}, error) {
	// 1. Marshal the concrete struct from the interface into a yaml.Node.
	//    This will give us a mapping node with the fields we want to inline (e.g., duration, retries).
	var configNode yaml.Node
	err := configNode.Encode(p.Config)
	if err != nil {
		return nil, err
	}

	// 2. Create the nodes for our "type" key-value pair.
	typeKeyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: "type",
	}
	typeValueNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: p.Type,
	}

	// 3. Prepend the "type" key-value pair to the beginning of the config node's content.
	//    The content of a mapping node is a flat slice of [key1, value1, key2, value2, ...].
	configNode.Content = append([]*yaml.Node{typeKeyNode, typeValueNode}, configNode.Content...)

	// 4. Return the modified node, which now contains the inlined fields plus the type.
	return &configNode, nil
}

// removeControllerType removes the controller type field from the JSON data
// and returns the type value. Removal is required so that we can unmarshall
// with KnownFields=true directly into the controller specific configuration
// and detect unsupported fields.
func removeControllerType(controllerNode *yaml.Node) (string, bool, error) {
	if len(controllerNode.Content) == 0 {
		// No controller was specified.
		return "", false, nil
	}

	if controllerNode.Kind != yaml.MappingNode {
		// Unexpected YAML structure.
		return "", false, errors.New("internal error: expected mapping node to locate controller type")
	}

	var newContent []*yaml.Node
	typeValue := ""
	otherValues := false
	for i := 0; i < len(controllerNode.Content); i += 2 {
		keyNode := controllerNode.Content[i]
		valNode := controllerNode.Content[i+1]

		// Keep only the fields that is not "type".
		if keyNode.Value != "type" {
			newContent = append(newContent, keyNode, valNode)
			otherValues = true
		} else {
			typeValue = valNode.Value
		}
	}
	controllerNode.Content = newContent
	return typeValue, otherValues, nil
}
