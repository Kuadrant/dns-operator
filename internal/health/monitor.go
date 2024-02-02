package health

import (
	"context"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Monitor struct {
	ProbeQueuers []*ProbeQueuer

	mux sync.Mutex
}

func NewMonitor() *Monitor {
	return &Monitor{
		ProbeQueuers: make([]*ProbeQueuer, 0),
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.V(3).Info("Starting health check monitor")

	<-ctx.Done()
	m.mux.Lock()
	defer m.mux.Unlock()

	logger.Info("Stopping health check monitor")

	for _, probeQueuer := range m.ProbeQueuers {
		probeQueuer.Stop()
	}

	return nil
}

var _ manager.Runnable = &Monitor{}

func (m *Monitor) HasProbe(id string) bool {
	m.mux.Lock()
	defer m.mux.Unlock()

	for _, probeQueuer := range m.ProbeQueuers {
		if probeQueuer.ID == id {
			return true
		}
	}

	return false
}

func (m *Monitor) UpdateProbe(id string, update func(*ProbeQueuer)) {
	m.mux.Lock()
	defer m.mux.Unlock()

	for _, probeQueuer := range m.ProbeQueuers {
		if probeQueuer.ID == id {
			update(probeQueuer)
		}
	}
}

func (m *Monitor) AddProbeQueuer(probeQueuer *ProbeQueuer) bool {
	m.mux.Lock()
	defer m.mux.Unlock()

	for _, existingProbe := range m.ProbeQueuers {
		if probeQueuer.ID == existingProbe.ID {
			return false
		}
	}

	m.ProbeQueuers = append(m.ProbeQueuers, probeQueuer)
	probeQueuer.Start()
	return true
}

func (m *Monitor) RemoveProbe(id string) {
	m.mux.Lock()
	defer m.mux.Unlock()

	updatedProbes := []*ProbeQueuer{}

	for _, probeQueuer := range m.ProbeQueuers {
		if probeQueuer.ID == id {
			probeQueuer.Stop()
		} else {
			updatedProbes = append(updatedProbes, probeQueuer)
		}
	}

	m.ProbeQueuers = updatedProbes
}
