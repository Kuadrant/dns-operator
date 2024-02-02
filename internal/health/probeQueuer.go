package health

import (
	"context"
	"time"

	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
)

type ProbeQueuer struct {
	ID string

	Interval                 time.Duration
	Protocol                 v1alpha1.HealthProtocol
	Path                     string
	IPAddress                string
	Host                     string
	Port                     int
	AdditionalHeaders        v1alpha1.AdditionalHeaders
	ExpectedResponses        []int
	AllowInsecureCertificate bool

	Notifier ProbeNotifier
	Queue    *QueuedProbeWorker

	cancel  context.CancelFunc
	started bool
	logger  logr.Logger
}

type ProbeResult struct {
	CheckedAt time.Time
	Reason    string
	Status    int
	Healthy   bool
}

type ProbeNotifier interface {
	Notify(ctx context.Context, result ProbeResult) (NotificationResult, error)
}

type NotificationResult struct {
	Requeue bool
}

func (p *ProbeQueuer) Start() {
	if p.started {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.logger = log.FromContext(ctx)

	p.logger.V(3).Info("Starting probe queuer", "id", p.ID)

	go func() {
		for {
			select {
			case <-time.After(p.Interval):
				p.Queue.EnqueueCheck(HealthRequest{
					Host:                     p.Host,
					Path:                     p.Path,
					Protocol:                 p.Protocol,
					Address:                  p.IPAddress,
					Port:                     p.Port,
					AdditionalHeaders:        p.AdditionalHeaders,
					ExpectedResponses:        p.ExpectedResponses,
					Notifier:                 p.Notifier,
					AllowInsecureCertificate: p.AllowInsecureCertificate,
				})
			case <-ctx.Done():
				return
			}
		}
	}()

	p.started = true
}

func (p *ProbeQueuer) Stop() {
	if !p.started {
		return
	}

	p.logger.V(3).Info("stopping probe", "id", p.ID)
	p.cancel()
}
