package planstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// PlanFunc is the type of function used by NotifyPlanChanged.
type PlanFunc func(p *plan.Plan)

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
