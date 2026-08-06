package controller

import (
	"testing"
	"time"
)

func TestClampTime(t *testing.T) {
	r := &BaseDNSRecordReconciler{}
	minRequeue := 5 * time.Second
	maxRequeue := 15 * time.Minute

	tests := []struct {
		name            string
		sinceTransition time.Duration
		want            time.Duration
	}{
		{
			name:            "returns minRequeue when just transitioned",
			sinceTransition: 0,
			want:            minRequeue,
		},
		{
			name:            "returns minRequeue when elapsed is less than min",
			sinceTransition: 2 * time.Second,
			want:            minRequeue,
		},
		{
			name:            "returns elapsed when between min and max",
			sinceTransition: 5 * time.Minute,
			want:            5 * time.Minute,
		},
		{
			name:            "returns maxRequeue when elapsed exceeds max",
			sinceTransition: 30 * time.Minute,
			want:            maxRequeue,
		},
		{
			name:            "returns maxRequeue when elapsed equals max",
			sinceTransition: maxRequeue,
			want:            maxRequeue,
		},
		{
			name:            "returns minRequeue when elapsed equals min",
			sinceTransition: minRequeue,
			want:            minRequeue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.ClampTime(minRequeue, maxRequeue, tt.sinceTransition)
			if got != tt.want {
				t.Errorf("ClampTime() = %v, want %v", got, tt.want)
			}
		})
	}
}
