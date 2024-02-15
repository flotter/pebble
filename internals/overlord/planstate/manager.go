package planstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// PlanFunc is the type of function used by NotifyPlanChanged.
type PlanFunc func(p *plan.Plan)

// LabelExists is the error returned by AppendLayer when a layer with that
// label already exists.
type LabelExists struct {
	Label string
}

func (e *LabelExists) Error() string {
	return fmt.Sprintf("layer %q already exists", e.Label)
}

type PlanManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	planLock     sync.Mutex
	plan         *plan.Plan
	planHandlers []PlanFunc
}

func NewManager(s *state.State, runner *state.TaskRunner, pebbleDir string) (*PlanManager, error) {
	manager := &PlanManager{
		state:     s,
		runner:    runner,
		pebbleDir: pebbleDir,
	}

	return manager, nil
}

// NotifyPlanChanged adds f to the list of functions that are called whenever
// the plan is updated.
func (m *PlanManager) NotifyPlanChanged(f PlanFunc) {
	fmt.Println("+ planstate NotifyPlanChanged")
	m.planHandlers = append(m.planHandlers, f)
}

func (m *PlanManager) updatePlan(plan *plan.Plan) {
	m.plan = plan
	for _, f := range m.planHandlers {
		f(plan)
	}
}

func (m *PlanManager) reloadPlan() error {
	plan, err := plan.ReadDir(m.pebbleDir)
	if err != nil {
		return err
	}
	m.updatePlan(plan)
	return nil
}

// Plan returns the configuration plan.
func (m *PlanManager) Plan() (*plan.Plan, error) {
	fmt.Println("+ planstate Plan")
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()
	return m.plan, nil
}

// AppendLayer appends the given layer to the plan's layers and updates the
// layer.Order field to the new order. If a layer with layer.Label already
// exists, return an error of type *LabelExists.
func (m *PlanManager) AppendLayer(layer *plan.Layer) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	index, _ := findLayer(m.plan.Layers, layer.Label)
	if index >= 0 {
		return &LabelExists{Label: layer.Label}
	}

	return m.appendLayer(layer)
}

func (m *PlanManager) appendLayer(layer *plan.Layer) error {
	newOrder := 1
	if len(m.plan.Layers) > 0 {
		last := m.plan.Layers[len(m.plan.Layers)-1]
		newOrder = last.Order + 1
	}

	newLayers := append(m.plan.Layers, layer)
	err := m.updatePlanLayers(newLayers)
	if err != nil {
		return err
	}
	layer.Order = newOrder
	return nil
}

func (m *PlanManager) updatePlanLayers(layers []*plan.Layer) error {
	combined, err := plan.CombineLayers(layers...)
	if err != nil {
		return err
	}
	plan := &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
	m.updatePlan(plan)
	return nil
}

// findLayer returns the index (in layers) of the layer with the given label,
// or returns -1, nil if there's no layer with that label.
func findLayer(layers []*plan.Layer, label string) (int, *plan.Layer) {
	for i, layer := range layers {
		if layer.Label == label {
			return i, layer
		}
	}
	return -1, nil
}

// CombineLayer combines the given layer with an existing layer that has the
// same label. If no existing layer has the label, append a new one. In either
// case, update the layer.Order field to the new order.
func (m *PlanManager) CombineLayer(layer *plan.Layer) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	index, found := findLayer(m.plan.Layers, layer.Label)
	if index < 0 {
		// No layer found with this label, append new one.
		return m.appendLayer(layer)
	}

	// Layer found with this label, combine into that one.
	combined, err := plan.CombineLayers(found, layer)
	if err != nil {
		return err
	}
	combined.Order = found.Order
	combined.Label = found.Label

	// Insert combined layer back into plan's layers list.
	newLayers := make([]*plan.Layer, len(m.plan.Layers))
	copy(newLayers, m.plan.Layers)
	newLayers[index] = combined
	err = m.updatePlanLayers(newLayers)
	if err != nil {
		return err
	}
	layer.Order = found.Order
	return nil
}

func (m *PlanManager) acquirePlan() (release func(), err error) {
	m.planLock.Lock()
	if m.plan == nil {
		err := m.reloadPlan()
		if err != nil {
			m.planLock.Unlock()
			return nil, err
		}
	}
	released := false
	release = func() {
		if !released {
			released = true
			m.planLock.Unlock()
		}
	}
	return release, nil
}

// Ensure implements StateManager.Ensure.
func (m *PlanManager) Ensure() error {
	return nil
}

// Ensure implements StateManager.StartUp.
func (m *PlanManager) StartUp() error {
	fmt.Println("+ planstate StartUp")
	m.planLock.Lock()
	defer m.planLock.Unlock()
	return m.reloadPlan()
}
