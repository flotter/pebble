package planstate

import (
	"sync"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

type PlanManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	mu           sync.Mutex
	plan         *plan.Plan
	handlers     []PlanChangedFunc
}

func NewManager(s *state.State, runner *state.TaskRunner, pebbleDir string) (*PlanManager, error) {
	manager := &PlanManager{
		state:     s,
		runner:    runner,
		pebbleDir: pebbleDir,
	}
	return manager, nil
}

// PlanChangedFunc defines a plan update handler. 
type PlanChangedFunc func(plan *plan.Plan, changedFacets []plan.PartName)

// NotifyPlanChanged adds a function to be called whenever the plan changes.
func (m *PlanManager) NotifyPlanChanged(f PlanChangedFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, f)
}

// AddFacet adds a plan facet to the plan. Facets have to be added by the
// respective managers, in the order or dependency.
func (m *PlanManager) AddFacet(facet plan.PartType) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.AddPartType(facet)
}

// Load the provided layers from storage and updates the plan to
// point to the combined view. ChangedFacets contains a list of
// facets that got non-nil values after the load.
func (m *PlanManager) Load() (changedFacets []plan.PartName, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.Load()
}

// ParseLayer creates a new layer from YAML data supplied. 
func (m *PlanManager) ParseLayer(order int, label string, data []byte) (*plan.Layer, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.ParseLayer(order, label, data)
}

// LayerExists can be used to decide between updating an existing layer or
// appending a new layer.
func (m *PlanManager) LayerExists(label string) bool {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.LayerExists(label)
}

// AppendLayer adds a new layer on top the existing layers, and then re-creates
// a new updated combined layer. The plan is updated to reflect the new
// combined layer.
func (m *PlanManager) AppendLayer(layer *plan.Layer) error {
    m.mu.Lock()

    defer m.mu.Unlock()
	changedFacets, err := m.plan.AppendLayer(layer)
	if err != nil {
		return err
	}

	m.notify(changedFacets)
	return nil
}

// UpdateLayer modifies an existing layer, and then re-creates a new updated
// combined layer. The plan is updated to reflect the new combined layer.
func (m *PlanManager) UpdateLayer(layer *plan.Layer) error {
    m.mu.Lock()
    defer m.mu.Unlock()

	changedFacets, err := m.plan.UpdateLayer(layer)
	if err != nil {
		return err
	}

	m.notify(changedFacets)
	return nil
 
}


// Facet provides a way to request a specific facet of the plan.
func (m *PlanManager) Facet(name plan.PartName) (facet plan.Part, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.Part(name)
}

// Plan provides a way to to return the complete plan. This can be used
// with marshal to produce a YAML view of the plan.
func (m *PlanManager) Plan() *plan.Layer {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.Plan()
}

// Ensure implements StateManager.Ensure.
func (m *PlanManager) Ensure() error {
	return nil
}

// StartUp implements StateManager.StartUp.
func (m *PlanManager) StartUp() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	changedFacets, err := m.Load()
	if err != nil {
		return err
	}

	m.notify(changedFacets)
	return nil
	
}

// notify requires the plan manager lock to already be in place
func (m *PlanManager) notify(changed []plan.PartName) {
	// Pass the combined layer and a list of all the changed facets to
	// each notification subscriber.
	for _, f := range m.handlers {
		f(m.plan, changed)
	}
}
