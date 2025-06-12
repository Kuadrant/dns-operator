package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

var (
	ErrLocalRecordNotPrimary         = fmt.Errorf("local record exists but is not primary")
	ErrLocalRecordDifferentRootHosts = fmt.Errorf("local record exists but has different root host")
)

func (r *DNSRecordReconciler) EnsureAuthoritativeRecord(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	primaryRecord, err := r.getPrimaryRecordForRecord(ctx, record)
	if err != nil {
		return nil, err
	}

	zr, err := r.getAuthoritativeRecordForPrimary(ctx, *primaryRecord)
	if err != nil && apierrors.IsNotFound(err) {
		return r.createAuthoritativeRecordForPrimary(ctx, *primaryRecord)
	}
	return zr, err
}

func (r *DNSRecordReconciler) getPrimaryRecordForRecord(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	// A primary record must exist on this cluster in the same namespace as the given record, with the same rootHost and have a valid provider
	primaryRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      record.Name,
			Namespace: record.Namespace,
		},
	}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(primaryRecord), primaryRecord); err != nil {
		return nil, err
	}

	if !primaryRecord.IsPrimary() {
		return nil, ErrLocalRecordNotPrimary
	}

	if primaryRecord.Spec.RootHost != record.Spec.RootHost {
		return nil, ErrLocalRecordDifferentRootHosts
	}

	return primaryRecord, nil
}

func (r *DNSRecordReconciler) getAuthoritativeRecordForPrimary(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecord := authoritativeRecordFor(record)

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(aRecord), aRecord); err != nil {
		return nil, fmt.Errorf("failed to get authoritative record: %w", err)
	}

	return aRecord, nil
}

func (r *DNSRecordReconciler) createAuthoritativeRecordForPrimary(ctx context.Context, record v1alpha1.DNSRecord) (*v1alpha1.DNSRecord, error) {
	aRecord := authoritativeRecordFor(record)

	if err := r.Client.Create(ctx, aRecord, &client.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create authoritative record: %w", err)
	}

	return aRecord, nil
}

func authoritativeRecordFor(rec v1alpha1.DNSRecord) *v1alpha1.DNSRecord {
	aRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toAuthoritativeRecordName(rec.Name),
			Namespace: rec.Namespace,
			Labels: map[string]string{
				v1alpha1.AuthoritativeRecordLabel: rec.Spec.RootHost,
			},
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: rec.Spec.RootHost,
			Endpoints: []*externaldns.Endpoint{
				{
					DNSName:    rec.Spec.RootHost,
					RecordType: "SOA",
					RecordTTL:  0,
				},
			},
			ProviderRef: &v1alpha1.ProviderRef{
				Name: rec.Status.ProviderRef.Name,
			},
		},
	}

	return aRecord
}

func toAuthoritativeRecordName(name string) string {
	return fmt.Sprintf("%s-%s", name, "authoritative")
}
