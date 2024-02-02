//go:build integration

package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	TestTimeoutMedium       = time.Second * 10
	TestTimeoutLong         = time.Second * 30
	TestRetryIntervalMedium = time.Millisecond * 250
	defaultNS               = "default"
	providerCredential      = "secretname"
)

type testHealthServer struct {
	Port int
}

func (s *testHealthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	endpoint := func(expectedCode int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(expectedCode)
		}
	}

	mux.HandleFunc("/healthy", endpoint(200))
	mux.HandleFunc("/unhealthy", endpoint(500))

	errCh := make(chan error)

	go func() {
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", s.Port), mux)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}
