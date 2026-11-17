//go:build unit

package controller

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func TestSecretZoneFilterSelectsAssignedZone(t *testing.T) {
	const (
		testNamespace = "test-ns"
		secretName    = "provider-secret"
		assignedZone  = "/hostedzone/Z1234567890"
	)

	testCases := []struct {
		name         string
		providerRef  string
		createSecret bool
		secretData   map[string][]byte
		zoneID       string
		wantSelects  bool
		wantFilter   string
		wantErr      bool
	}{
		{
			name:        "no provider secret assigned",
			providerRef: "",
			zoneID:      assignedZone,
			wantSelects: true,
		},
		{
			name:         "secret without ZONE_ID_FILTER selects every zone",
			providerRef:  secretName,
			createSecret: true,
			secretData:   map[string][]byte{"AWS_REGION": []byte("eu-west-1")},
			zoneID:       assignedZone,
			wantSelects:  true,
		},
		{
			name:         "filter matches the assigned zone",
			providerRef:  secretName,
			createSecret: true,
			secretData:   map[string][]byte{v1alpha1.ZoneIDFilterKey: []byte(assignedZone)},
			zoneID:       assignedZone,
			wantSelects:  true,
			wantFilter:   assignedZone,
		},
		{
			name:         "bare zone id matches a full assigned zone id",
			providerRef:  secretName,
			createSecret: true,
			secretData:   map[string][]byte{v1alpha1.ZoneIDFilterKey: []byte("Z1234567890")},
			zoneID:       assignedZone,
			wantSelects:  true,
			wantFilter:   "Z1234567890",
		},
		{
			name:         "whitespace around the filter is trimmed",
			providerRef:  secretName,
			createSecret: true,
			secretData:   map[string][]byte{v1alpha1.ZoneIDFilterKey: []byte("  " + assignedZone + "  ")},
			zoneID:       assignedZone,
			wantSelects:  true,
			wantFilter:   assignedZone,
		},
		{
			name:         "filter selects a different zone than the one assigned",
			providerRef:  secretName,
			createSecret: true,
			secretData:   map[string][]byte{v1alpha1.ZoneIDFilterKey: []byte("/hostedzone/ZOTHERZONE")},
			zoneID:       assignedZone,
			wantSelects:  false,
			wantFilter:   "/hostedzone/ZOTHERZONE",
		},
		{
			name:         "filter set on a record with no zone assigned yet",
			providerRef:  secretName,
			createSecret: true,
			secretData:   map[string][]byte{v1alpha1.ZoneIDFilterKey: []byte(assignedZone)},
			zoneID:       "",
			wantSelects:  false,
			wantFilter:   assignedZone,
		},
		{
			name:         "missing provider secret surfaces the error",
			providerRef:  secretName,
			createSecret: false,
			zoneID:       assignedZone,
			wantSelects:  true,
			wantErr:      true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			var objects []client.Object
			if tt.createSecret {
				objects = append(objects, &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: testNamespace},
					Data:       tt.secretData,
				})
			}
			fakeClient := fake.NewClientBuilder().WithObjects(objects...).Build()

			accessor := &DNSRecord{
				DNSRecord: &v1alpha1.DNSRecord{
					ObjectMeta: metav1.ObjectMeta{Name: "test-record", Namespace: testNamespace},
					Status: v1alpha1.DNSRecordStatus{
						ZoneID:      tt.zoneID,
						ProviderRef: v1alpha1.ProviderRef{Name: tt.providerRef},
					},
				},
			}

			reconciler := &BaseDNSRecordReconciler{}
			selects, filter, err := reconciler.secretZoneFilterSelectsAssignedZone(ctx, fakeClient, accessor)

			if tt.wantErr {
				if err == nil {
					t.Fatal("secretZoneFilterSelectsAssignedZone() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("secretZoneFilterSelectsAssignedZone() unexpected error: %v", err)
			}
			if selects != tt.wantSelects {
				t.Errorf("selects = %v, want %v", selects, tt.wantSelects)
			}
			if filter != tt.wantFilter {
				t.Errorf("filter = %q, want %q", filter, tt.wantFilter)
			}
		})
	}
}
