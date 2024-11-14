package probes_test

import (
	"context"
	"fmt"
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

	var testHeaders = []v1alpha1.AdditionalHeader{
		{Name: "first", Value: "test"},
		{Name: "second", Value: "test"},
	}

	var validateHeaders = func(r *http.Request, headers v1alpha1.AdditionalHeaders) error {
		for _, kv := range headers {
			header := r.Header.Get(kv.Name)
			if header != kv.Value {
				return fmt.Errorf("expected header %s with value %s but got value %s ", kv.Name, kv.Value, header)
			}
		}
		return nil
	}

	testCases := []struct {
		Name               string
		Transport          func(headers v1alpha1.AdditionalHeaders) probes.RoundTripperFunc
		ProbeConfig        func() *v1alpha1.DNSHealthCheckProbe
		ProbeHeaders       v1alpha1.AdditionalHeaders
		Validate           func(t *testing.T, results []probes.ProbeResult, tt *testTransport, expectedCalls int)
		Ctx                context.Context
		ExpectedProbeCalls int
	}{
		{
			Name: "test health check success",
			Transport: func(expectedHeaders v1alpha1.AdditionalHeaders) probes.RoundTripperFunc {
				return func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: 200,
					}, validateHeaders(r, expectedHeaders)
				}
			},
			ExpectedProbeCalls: 5,
			ProbeConfig: func() *v1alpha1.DNSHealthCheckProbe {
				return testProbe.DeepCopy()
			},
			ProbeHeaders: testHeaders,
			Validate: func(t *testing.T, results []probes.ProbeResult, tt *testTransport, expectedCalls int) {
				if len(results) != expectedCalls {
					t.Fatalf("expected %v results got %v", expectedCalls, len(results))
				}
				lastResult := results[expectedCalls-1]
				// get the last probe result
				if lastResult.Healthy == false {
					t.Fatalf("expected the result of the probe to be healthy but it was not")
				}
				if !lastResult.CheckedAt.After(results[len(results)-2].CheckedAt.Time) {
					t.Fatalf("result checked at should be after the previous result checkAt ")
				}
				if !lastResult.CheckedAt.After(lastResult.PreviousCheck.Time) {
					t.Fatalf("result checked at should be after the previousCheck")
				}
				if lastResult.Status != 200 {
					t.Fatalf("expected the result status to be 200 but got %v", lastResult.Status)
				}
			},
		},
		{
			Name: "test healthcheck failure ",
			Transport: func(headers v1alpha1.AdditionalHeaders) probes.RoundTripperFunc {
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
			Validate: func(t *testing.T, results []probes.ProbeResult, tt *testTransport, expectedCalls int) {
				if len(results) != expectedCalls {
					t.Fatalf("expected %v results got %v", expectedCalls, len(results))
				}
				lastResult := results[expectedCalls-1]
				if lastResult.Healthy != false {
					t.Fatalf("expected the result of the probe to be un-healthy but it was not")
				}
				if lastResult.Status != 503 {
					t.Fatalf("expected result status to be 503 but got %v", lastResult.Status)
				}
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			probeConfig := test.ProbeConfig()
			w := probes.NewProbe(test.ProbeHeaders)
			tTransport := &testTransport{}
			tTransport.f = test.Transport(test.ProbeHeaders)
			w.Transport = tTransport.countedHTTP
			ctx, cancel := context.WithCancel(context.TODO())
			//setup our channel
			exChan := w.ExecuteProbe(ctx, probeConfig)
			wait := sync.WaitGroup{}
			wait.Add(test.ExpectedProbeCalls)
			// read from our channel
			executedProbes := []probes.ProbeResult{}
			go func() {
				calls := 0
				for result := range exChan {
					calls++
					t.Logf("channel read %v", calls)
					executedProbes = append(executedProbes, result)
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
			test.Validate(t, executedProbes, tTransport, test.ExpectedProbeCalls)
		})
	}
}
