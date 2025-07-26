package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type DNSRecordDelegationHelper struct {
	client.Client
}

// EnsureAuthoritativeRecord ensures that an authoritative DNSRecord exists in the given DNSRecords(record) namespace.
// An authoritative DNSRecord must have the delegation label with a value matching the given DNSRecords(record) rootHost.
// i.e. kuadrant.io/delegation-authoritative-record=<record.spec.rootHost>
func (r *DNSRecordDelegationHelper) EnsureAuthoritativeRecord(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecord, err := r.getAuthoritativeRecordFor(ctx, record)
	if err != nil {
		return nil, err
	}
	if aRecord != nil {
		return aRecord, err
	}
	return r.createAuthoritativeRecordFor(ctx, record)
}

func (r *DNSRecordDelegationHelper) getAuthoritativeRecordFor(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecords := v1alpha1.DNSRecordList{}

	labelSelector, err := labels.Parse(fmt.Sprintf("%s=%s", v1alpha1.DelegationAuthoritativeRecordLabel, record.Spec.RootHost))
	if err != nil {
		return nil, err
	}

	if err := r.Client.List(ctx, &aRecords, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return nil, fmt.Errorf("failed to get authoritative record: %w", err)
	}

	if len(aRecords.Items) > 0 {
		return &aRecords.Items[0], nil
	}

	return nil, nil
}

func (r *DNSRecordDelegationHelper) createAuthoritativeRecordFor(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecord := authoritativeRecordFor(record)

	if err := r.Client.Create(ctx, aRecord, &client.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create authoritative record: %w", err)
	}

	return aRecord, nil
}

func authoritativeRecordFor(rec v1alpha1.DNSRecord) *v1alpha1.DNSRecord {
	return &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "delegation-authoritative-record-",
			Namespace:    rec.Namespace,
			Labels: map[string]string{
				v1alpha1.DelegationAuthoritativeRecordLabel: rec.Spec.RootHost,
			},
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: rec.Spec.RootHost,
		},
	}
}
