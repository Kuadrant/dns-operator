package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
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

func TestSpecOrMetadataChangedPredicate_Update(t *testing.T) {
	p := specOrMetadataChangedPredicate{}

	tests := []struct {
		name string
		old  *v1alpha1.DNSRecord
		new  *v1alpha1.DNSRecord
		want bool
	}{
		{
			name: "nil old object returns false",
			new:  &v1alpha1.DNSRecord{},
			want: false,
		},
		{
			name: "nil new object returns false",
			old:  &v1alpha1.DNSRecord{},
			want: false,
		},
		{
			name: "generation changed returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 2}},
			want: true,
		},
		{
			name: "finalizer added returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Finalizers: []string{"kuadrant.io/dns-record"}}},
			want: true,
		},
		{
			name: "finalizer removed returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Finalizers: []string{"kuadrant.io/dns-record"}}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			want: true,
		},
		{
			name: "label added returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"provider": "inmemory"}}},
			want: true,
		},
		{
			name: "label value changed returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"provider": "inmemory"}}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"provider": "aws"}}},
			want: true,
		},
		{
			name: "annotation added returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Annotations: map[string]string{"key": "value"}}},
			want: true,
		},
		{
			name: "status-only change returns false",
			old: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, ResourceVersion: "100"},
			},
			new: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, ResourceVersion: "101"},
				Status:     v1alpha1.DNSRecordStatus{OwnerID: "abc123"},
			},
			want: false,
		},
		{
			name: "no changes returns false",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := event.UpdateEvent{}
			if tt.old != nil {
				e.ObjectOld = tt.old
			}
			if tt.new != nil {
				e.ObjectNew = tt.new
			}
			if got := p.Update(e); got != tt.want {
				t.Errorf("Update() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposedPredicate_DeletingObject(t *testing.T) {
	composed := predicate.Or(specOrMetadataChangedPredicate{}, deletingPredicate{})
	now := metav1.Now()

	tests := []struct {
		name string
		old  *v1alpha1.DNSRecord
		new  *v1alpha1.DNSRecord
		want bool
	}{
		{
			name: "deleting object with no other changes returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now}},
			want: true,
		},
		{
			name: "non-deleting status-only change returns false",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     v1alpha1.DNSRecordStatus{OwnerID: "abc123"},
			},
			want: false,
		},
		{
			name: "deleting object with finalizer change returns true",
			old:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, Finalizers: []string{"kuadrant.io/dns-record"}, DeletionTimestamp: &now}},
			new:  &v1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}
			if got := composed.Update(e); got != tt.want {
				t.Errorf("Update() = %v, want %v", got, tt.want)
			}
		})
	}
}
