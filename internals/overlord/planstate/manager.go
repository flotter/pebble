package planstate

import (
	"fmt"
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
	handlers     []PlanFunc
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
type PlanChangedFunc func(p *plan.Plan, changedFacets []plan.PartName)

// NotifyPlanChanged adds a function to be called whenever the plan changes.
func (m *PlanManager) NotifyPlanChanged(f PlanFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.planHandlers = append(m.planHandlers, f)
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
func (m *PlanManager) Load() (changedFacets []PartName, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.Load()
}

// ParseLayer creates a new layer from YAML data supplied. 
func (m *PlanManager) ParseLayer(order int, label string, data []byte) (*Layer, error) {
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
func (m *PlanManager) AppendLayer(layer *Layer) (changed []PartName, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.AppendLayer(layer)
}

// UpdateLayer modifies an existing layer, and then re-creates a new updated
// combined layer. The plan is updated to reflect the new combined layer.
func (m *PlanManager) UpdateLayer(layer *Layer) (changed []PartName, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.UpdateLayer(layer)
}


// Facet provides a way to request a specific facet of the plan.
func (m *PlanManager) Facet(name plan.PartName) (facet plan.Part, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan.Part(name)
}

// Plan provides a way to to return the complete plan. This can be used
// with marshal to produce a YAML view of the plan.
func (m *PlanManager) Plan() *plan.Plan {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.plan
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
