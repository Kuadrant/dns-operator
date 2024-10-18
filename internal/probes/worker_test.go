package probes_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/probes"
)

type testTransport struct {
	calls int
	f     func(r *http.Request) (*http.Response, error)
}

func (tt *testTransport) countedHTTP(r *http.Request) (*http.Response, error) {
	tt.calls++
	return tt.f(r)
}

func TestWorker_ProbeSuccess(t *testing.T) {

	var testProbe = &v1alpha1.DNSHealthCheckProbe{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: v1alpha1.DNSHealthCheckProbeSpec{
			Hostname:         "example.com",
			Address:          "1.2.1.1",
			Path:             "/test",
			Interval:         v1.Duration{Duration: time.Second},
			Port:             80,
			Protocol:         v1alpha1.HttpProtocol,
			FailureThreshold: 3,
		},
		Status: v1alpha1.DNSHealthCheckProbeStatus{},
	}

	testCases := []struct {
		Name               string
		Transport          func() probes.RoundTripperFunc
		ProbeConfig        func() *v1alpha1.DNSHealthCheckProbe
		ProbeHeaders       v1alpha1.AdditionalHeaders
		Validate           func(t *testing.T, probe *v1alpha1.DNSHealthCheckProbe, tt *testTransport, expectedCalls int)
		Ctx                context.Context
		ExpectedProbeCalls int
	}{
		{
			Name: "test health check success",
			Transport: func() probes.RoundTripperFunc {
				return func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: 200,
					}, nil
				}
			},
			ExpectedProbeCalls: 5,
			ProbeConfig: func() *v1alpha1.DNSHealthCheckProbe {
				return testProbe.DeepCopy()
			},
			Validate: func(t *testing.T, probe *v1alpha1.DNSHealthCheckProbe, tt *testTransport, expectedCalls int) {
				if probe.Status.ConsecutiveFailures != 0 {
					t.Fatalf("expected no failures but got %v", probe.Status.ConsecutiveFailures)
				}
				if probe.Status.Healthy == nil || *probe.Status.Healthy != true {
					t.Fatalf("expected the probe to be healthy")
				}
				if tt.calls != expectedCalls {
					t.Fatalf("expected the number of health probe http calls to be %v but got %v", expectedCalls, tt.calls)
				}
			},
		},
		{
			Name: "test healthcheck failure ",
			Transport: func() probes.RoundTripperFunc {
				return func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: 503,
					}, nil
				}
			},
			ProbeConfig: func() *v1alpha1.DNSHealthCheckProbe {
				return testProbe.DeepCopy()
			},
			ExpectedProbeCalls: 3,
			Validate: func(t *testing.T, probe *v1alpha1.DNSHealthCheckProbe, tt *testTransport, expectedCalls int) {
				if probe.Status.ConsecutiveFailures != expectedCalls {
					t.Fatalf("expected %v failures but got %v", expectedCalls, probe.Status.ConsecutiveFailures)
				}
				if probe.Status.Healthy == nil || *probe.Status.Healthy != false {
					t.Fatalf("expected the probe to be unhealthy")
				}
				if tt.calls != expectedCalls {
					t.Fatalf("expected the number of health probe http calls to be %v but got %v", expectedCalls, tt.calls)
				}
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			probeConfig := test.ProbeConfig()
			w := probes.NewProbe(probeConfig, test.ProbeHeaders)
			tTransport := &testTransport{}
			tTransport.f = test.Transport()
			w.Transport = tTransport.countedHTTP
			ctx, cancel := context.WithCancel(context.TODO())
			//setup our channel
			exChan := w.ExecuteProbe(ctx, probeConfig)
			wait := sync.WaitGroup{}
			wait.Add(test.ExpectedProbeCalls)
			// read from our channel
			go func() {
				calls := 0
				for range exChan {
					calls++
					t.Logf("channel read %v", calls)
					wait.Done()
				}
			}()
			wait.Wait()
			// allow us to wait for the channel to close based on context cancel
			wait.Add(1)
			go func() {
				<-exChan
				t.Logf("channel closed ")
				wait.Done()
			}()
			// cancel our context and wait for the probe to exit
			cancel()
			wait.Wait()
			test.Validate(t, probeConfig, tTransport, test.ExpectedProbeCalls)
		})
	}
}
