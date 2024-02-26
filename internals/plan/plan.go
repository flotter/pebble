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
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FormatError is the error returned when a part has a format error, such as
// a missing "override" field.
type FormatError struct {
	Message string
}

func (e *FormatError) Error() string {
	return e.Message
}

// LabelExists is the error returned by AppendLayer when a layer with that
// label already exists.
type LabelExists struct {
	Label string
}

func (e *LabelExists) Error() string {
	return fmt.Sprintf("layer %q already exists", e.Label)
}

// LabelMissing is the error returned by UpdateLayer when the requested layer
// does not yet exist.
type LabelMissing struct {
	Label string
}

func (e *LabelMissing) Error() string {
	return fmt.Sprintf("layer %q not found", e.Label)
}

// PartName uniquely describes a top-level part key, as used in the plan schema.
type PartName string

// PartType describes a specific part type.
type PartType interface {
	// The top level YAML key for this part.
	Key() PartName

	// The parts on which this part type is dependant. They have to be
	// available by the time this type is registered.
	Wants() (parts []PartName)

	// New create and return a new concrete part type instance.
	New() Part
}

// Override specifies the layer override mechanism for an object.
type Override string

const (
	UnknownOverride Override = ""
	MergeOverride   Override = "merge"
	ReplaceOverride Override = "replace"
)

// Part defines an externally defined data structure that is compatible as
// part of the global plan. A Part consists of Entries.
type Part interface {
	// ValidatePart allows adding additional rules beyond the typical YAML
	// schema type mapping logic. This method should only be concerned with
	// the part details in isolation, and is called immediately after the
	// part data is unmarshalled.
	ValidatePart() error

	// ValidatePlan performs validation on the complete plan by looking at
	// the combined plan layer. Parts that does depend on other parts can
	// use the combined layer to access details from other parts for
	// validation purposes.
	//
	// This method is only called when the plan (combined layer) is about to
	// get updated. It is not called when a individual layer is parsed.
	ValidatePlan(combined *Layer) error

	// Combine another part into itself, taking the override attribute for
	// each part entry into account where applicable.
	Combine(other Part) error

	// IsNonEmpty returns True if the unmarshal of YAML data was resulted in
	// the content of the part getting updated, otherwise it returns False.
	// This is used at runtime to introspect a layer and determine which parts
	// actually contains any entries, so we can produce a finer grained
	// notification system.
	IsNonEmpty() bool
}

type Layer struct {
	Order       int    `yaml:"-"`
	Label       string `yaml:"-"`
	Summary     string `yaml:"summary,omitempty"`
	Description string `yaml:"description,omitempty"`

	Parts      map[PartName]Part `yaml:,inline`
	PartsOrder []PartName        `yaml:"-"`
}

// UnmarshalYAML deals with the fact that the Parts map is already created before
// we start the unmarshal. This allows us to know the Parts in advance, which
// can now help us unmarshal the custom plan.
func (l *Layer) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a mapping node, got %v", value.Kind)
	}

	for i := 0; i < len(value.Content)-1; i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]

		switch keyNode.Value {
		case "summary":
			l.Summary = valNode.Value
		case "description":
			l.Description = valNode.Value
		default:
			found := false
			for key, _ := range l.Parts {
				if string(key) == keyNode.Value {
					found = true
					err := valNode.Decode(l.Parts[key])
					if err != nil {
						return fmt.Errorf(
							"cannot parse plan part %q: %w",
							keyNode.Value,
							err,
						)
					}
					break
				}
			}
			if !found {
				return fmt.Errorf("cannot find plan part %q", keyNode.Value)
			}
		}
	}

	return nil
}

// MarshalYAML presents a layer in YAML
func (l *Layer) MarshalYAML() (interface{}, error) {
	result := make(map[string]interface{})
	for key, part := range l.Parts {
		result[string(key)] = part
	}
	return result, nil
}

// Part returns a part from a specific layer. This is a helper method
// to allow parts to validate a plan by looking at other parts. This
// should only be used in the context of the combined part layer.
func (l *Layer) Part(name PartName) (part Part, err error) {
	if part, ok := l.Parts[name]; ok {
		return part, nil
	}
	return nil, fmt.Errorf("cannot find part %q in layer", name)
}

// NonEmptyParts provides an ordered list of parts which actually contains
// information following an unmarshal attempt to load a layer.
func (l *Layer) NonEmptyParts() []PartName {
	var updated []PartName
	for _, name := range l.PartsOrder {
		if l.Parts[name].IsNonEmpty() {
			updated = append(updated, name)
		}
	}
	return updated
}

type Plan struct {
	baseDir          string
	orderedPartTypes []PartType

	Layers   []*Layer
	Combined *Layer
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
	// Perform add order validation. An error produced here
	// means the order in which parts are added at program
	// startup is wrong.
	for _, want := range partType.Wants() {
		found := false
		for _, pType := range p.orderedPartTypes {
			if want == pType.Key() {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf(
				"cannot find plan part dependency %q",
				want,
			)
		}
	}

	p.orderedPartTypes = append(p.orderedPartTypes, partType)
	return nil
}

// newLayer create a new layer consisting of all the added parts which form
// part of the plan at runtime. Note that parts are ordered to ensure part
// validation (which may require details from other parts) always work.
func (p *Plan) newLayer() *Layer {
	layer := &Layer{
		Parts: make(map[PartName]Part),
	}
	for _, ptype := range p.orderedPartTypes {
		key := ptype.Key()
		layer.PartsOrder = append(layer.PartsOrder, key)
		layer.Parts[key] = ptype.New()
	}
	return layer
}

// Load reads the configuration layers from the "layers" sub-directory in
// baseDir, and updates the plan, dropping any previous ephermeral layers. If
// the "layers" sub-directory doesn't exist, a valid empty Plan exist. Any
// part that updated on load will be in the returned changed list.
func (p *Plan) Load() (changed []PartName, err error) {
	layersDir := filepath.Join(p.baseDir, "layers")
	_, err = os.Stat(layersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	layers, err := p.readLayersDir(layersDir)
	if err != nil {
		return nil, err
	}
	combined, err := p.combineLayers(layers...)
	if err != nil {
		return nil, err
	}
	err = p.validate(combined)
	if err != nil {
		return nil, err
	}

	p.Layers = layers
	p.Combined = combined

	// Which layer parts actually contains data?
	updatedParts := combined.NonEmptyParts()

	return updatedParts, nil
}

func (p *Plan) ParseLayer(order int, label string, data []byte) (*Layer, error) {
	layer := p.newLayer()
	dec := yaml.NewDecoder(bytes.NewBuffer(data))
	dec.KnownFields(true)
	err := dec.Decode(layer)
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}

	// Validate each part in order
	for _, key := range layer.PartsOrder {
		part := layer.Parts[key]
		err = part.ValidatePart()
		if err != nil {
			return nil, fmt.Errorf("cannot validate part %v: %w", key, err)
		}
	}

	layer.Order = order
	layer.Label = label
	return layer, nil
}

// combineLayers combine the given layers in turn (in the supplied order)
// into one layer. Each layer part merge will consults its local override
// attributes to either replace or merge an individual part entry.
func (p *Plan) combineLayers(layers ...*Layer) (*Layer, error) {
	combined := p.newLayer()
	if len(layers) == 0 {
		return combined, nil
	}
	last := layers[len(layers)-1]
	combined.Summary = last.Summary
	combined.Description = last.Description

	// Combine all the layers into one
	for _, layer := range layers {
		for _, partName := range combined.PartsOrder {
			err := combined.Parts[partName].Combine(layer.Parts[partName])
			if err != nil {
				return nil, fmt.Errorf(
					"cannot combine plan part %q from layer %q: %w",
					partName,
					layer.Label,
					err,
				)
			}
		}
	}
	return combined, nil
}

// Validate performs plan validation of the supplied combined layer. This
// layer must be a flattened representation of the entire plan. This method
// cannot be used on a separate layer as the layer on its own may not include
// the required plan dependencies needed for plan validation.
func (p *Plan) validate(combined *Layer) error {
	for _, partName := range combined.PartsOrder {
		err := combined.Parts[partName].ValidatePlan(combined)
		if err != nil {
			return fmt.Errorf(
				"cannot validate plan part %q: %w",
				partName,
				err,
			)
		}
	}
	return nil
}

var fnameExp = regexp.MustCompile("^([0-9]{3})-([a-z](?:-?[a-z0-9]){2,}).yaml$")

func (p *Plan) readLayersDir(dirname string) ([]*Layer, error) {
	finfos, err := ioutil.ReadDir(dirname)
	if err != nil {
		// Errors from package os generally include the path.
		return nil, fmt.Errorf("cannot read layers directory: %w", err)
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
			return nil, fmt.Errorf("cannot read layer file: %w", err)
		}
		label := match[2]
		order, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("internal error: filename regexp is wrong: %w", err)
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

// findLayer returns the index (in layers) of the layer with the given label,
// or returns -1, nil if there's no layer with that label.
func (p *Plan) findLayer(label string) (int, *Layer) {
	for i, layer := range p.Layers {
		if layer.Label == label {
			return i, layer
		}
	}
	return -1, nil
}

// LayerExists provides a way to determine if an AppendLayer or UpdateLayer
// operation is suitable. Updating a layer may only be performed if a layer
// with a matching label already exist. If the layer does not exist, the
// requester may decide to perform a layer AppendLayer instead.
func (p *Plan) LayerExists(label string) bool {
	_, layer := p.findLayer(label)
	return layer != nil
}

// AppendLayer appends the given layer to the plan's layers and updates the
// layer.Order field to the new order. If a layer with layer.Label already
// exists, return an error of type *LabelExists.
func (p *Plan) AppendLayer(layer *Layer) (changed []PartName, err error) {
	index, _ := p.findLayer(layer.Label)
	if index >= 0 {
		return nil, &LabelExists{Label: layer.Label}
	}

	// Which layer parts actually contains data?
	updatedParts := layer.NonEmptyParts()

	// The provided append layer order number is ignored since the append
	// operation implies a new order, determined by the number of present
	// layers.
	newOrder := 1
	if len(p.Layers) > 0 {
		last := p.Layers[len(p.Layers)-1]
		newOrder = last.Order + 1
	}

	newLayers := append(p.Layers, layer)
	combined, err := p.combineLayers(newLayers...)
	if err != nil {
		return nil, err
	}

	// Validate the combined plan
	err = p.validate(combined)
	if err != nil {
		return nil, err
	}

	// Publish the new plan
	p.Layers = newLayers
	p.Combined = combined

	// The layer provided must reflect the newly allocated order number
	// after the plan was updated (as per the original requirements).
	layer.Order = newOrder
	return updatedParts, nil
}

// UpdateLayer combines the given layer with an existing layer that has the
// same label. If no existing layer has the label, return the error of type
// *LabelMissing. Update the layer.Order field to the new order.
func (p *Plan) UpdateLayer(layer *Layer) (changed []PartName, err error) {
	index, found := p.findLayer(layer.Label)
	if index < 0 {
		return nil, &LabelMissing{Label: layer.Label}
	}

	// Which layer parts actually contains data?
	updatedParts := layer.NonEmptyParts()

	// Layer found with this label, combine into that one.
	updated, err := p.combineLayers(found, layer)
	if err != nil {
		return nil, err
	}
	updated.Order = found.Order
	updated.Label = found.Label

	// Insert combined layer back into plan's layers list.
	newLayers := make([]*Layer, len(p.Layers))
	copy(newLayers, p.Layers)
	newLayers[index] = updated

	combined, err := p.combineLayers(newLayers...)
	if err != nil {
		return nil, err
	}

	// Validate the combined plan
	err = p.validate(combined)
	if err != nil {
		return nil, err
	}

	// Publish the new plan
	p.Layers = newLayers
	p.Combined = combined

	// The layer provided must reflect the newly allocated order number
	// after the plan was updated (as per the original requirements).
	layer.Order = found.Order
	return updatedParts, nil
}

// Part returns the combined plan view relating to a specific part.
func (p *Plan) Part(name PartName) (part Part, err error) {
	if part, ok := p.Combined.Parts[name]; ok {
		return part, nil
	}
	return nil, fmt.Errorf("cannot find part %q in plan", name)
}

// Plan returns the combined plan view
func (p *Plan) Plan() (combined *Layer) {
	return p.Combined
}
