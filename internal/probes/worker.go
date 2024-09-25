package probes

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type Worker struct {
	probe  *v1alpha1.DNSHealthCheckProbe
	client client.Client
}

func NewWorker(k8sClient client.Client, probe *v1alpha1.DNSHealthCheckProbe) *Worker {
	return &Worker{
		probe:  probe,
		client: k8sClient,
	}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		keepWorking := true
		// exit when context is closed or hits a deadline, or if ProcessProbeJob returns false
		for ctx.Err() == nil && keepWorking {
			keepWorking = ProcessProbeJob(ctx, w.client, w.probe)
		}
	}()
}

type WorkerManager struct {
	workers map[string]*Worker
}

func NewWorkerManager() *WorkerManager {
	return &WorkerManager{
		workers: map[string]*Worker{},
	}
}

func (m *WorkerManager) EnsureProbeWorker(ctx context.Context, k8sClient client.Client, probeCR *v1alpha1.DNSHealthCheckProbe) {
	logger := log.FromContext(ctx)
	if _, ok := m.workers[keyForProbe(probeCR)]; ok {
		logger.V(1).Info("worker already exists")
		return
	}
	logger.V(1).Info("starting a new worker")

	worker := NewWorker(k8sClient, probeCR)
	worker.Start(ctx)
	m.workers[keyForProbe(probeCR)] = worker
}

func keyForProbe(probe *v1alpha1.DNSHealthCheckProbe) string {
	return fmt.Sprintf("%s/%s", probe.Name, probe.Namespace)
}
