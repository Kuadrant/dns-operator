package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"

	externaldnsplan "github.com/kuadrant/dns-operator/internal/external-dns/plan"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/types"
)

type BaseDNSRecordReconciler struct {
	Scheme          *runtime.Scheme
	ProviderFactory provider.Factory
	DelegationRole  string
	Group           *types.Group
}

func (r *BaseDNSRecordReconciler) IsPrimary() bool {
	return r.DelegationRole == DelegationRolePrimary
}

func (r *BaseDNSRecordReconciler) IsSecondary() bool {
	return r.DelegationRole == DelegationRoleSecondary
}

// setLogger Updates the given Logger with record/zone metadata from the given DNSRecord.
// returns the context with the updated logger set on it, and the updated logger itself.
func (r *BaseDNSRecordReconciler) setLogger(ctx context.Context, logger logr.Logger, dnsRecord DNSRecordAccessor) (context.Context, logr.Logger) {
	logger = logger.
		WithValues("rootHost", dnsRecord.GetRootHost()).
		WithValues("ownerID", dnsRecord.GetOwnerID()).
		WithValues("zoneID", dnsRecord.GetZoneID()).
		WithValues("zoneDomainName", dnsRecord.GetZoneDomainName()).
		WithValues("delegationRole", r.DelegationRole)
	return log.IntoContext(ctx, logger), logger
}

// getDNSProvider returns a Provider configured for the given DNSRecord
// If no zone/id/domain has been assigned to the given record, an error is thrown.
// If no owner has been assigned to the given record, an error is thrown.
// If the provider can't be initialised, an error is thrown.
func (r *BaseDNSRecordReconciler) getDNSProvider(ctx context.Context, dnsRecord DNSRecordAccessor) (provider.Provider, error) {
	var err error
	if !dnsRecord.HasOwnerIDAssigned() {
		err = errors.Join(fmt.Errorf("has no ownerID assigned"))
	}
	if !dnsRecord.HasDNSZoneAssigned() {
		err = errors.Join(fmt.Errorf("has no DNSZone assigned"))
	}
	if err != nil {
		return nil, err
	}
	providerConfig := provider.Config{
		HostDomainFilter: externaldnsendpoint.NewDomainFilter([]string{dnsRecord.GetRootHost()}),
		DomainFilter:     externaldnsendpoint.NewDomainFilter([]string{dnsRecord.GetZoneDomainName()}),
		ZoneTypeFilter:   externaldnsprovider.NewZoneTypeFilter(""),
		ZoneIDFilter:     externaldnsprovider.NewZoneIDFilter([]string{dnsRecord.GetZoneID()}),
	}
	return r.ProviderFactory.ProviderFor(ctx, dnsRecord.GetDNSRecord(), providerConfig)
}

// deleteRecord deletes record(s) in the DNSProvider(i.e. route53) zone (dnsRecord.GetZoneID()).
func deleteRecord(ctx context.Context, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)

	hadChanges, err := applyChanges(ctx, dnsRecord, dnsProvider, true)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(err.Error(), "notFound") {
			logger.Info("Record not found in zone, continuing")
			return false, nil
		} else if strings.Contains(err.Error(), "no endpoints") {
			logger.Info("DNS record had no endpoint, continuing")
			return false, nil
		}
		return false, err
	}
	logger.Info("Deleted DNSRecord in zone")

	return hadChanges, nil
}

// publishRecord publishes record(s) to the DNSProvider(i.e. route53) zone (dnsRecord.GetZoneID()).
// returns if it had changes, if record is healthy and an error. If had no changes - the healthy bool can be ignored
func publishRecord(ctx context.Context, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider) (bool, error) {
	logger := log.FromContext(ctx)
	hadChanges, err := applyChanges(ctx, dnsRecord, dnsProvider, false)
	if err != nil {
		return hadChanges, err
	}
	logger.Info("Published DNSRecord to zone")

	return hadChanges, nil
}

// applyChanges creates the Plan and applies it to the registry. Returns true only if the Plan had no errors and there were changes to apply.
// The error is nil only if the changes were successfully applied or there were no changes to be made.
func applyChanges(ctx context.Context, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider, isDelete bool) (bool, error) {
	logger := log.FromContext(ctx)
	//ToDo We can't use GetRootHost() here as it currently removes any wildcard prefix which needs to be maintained in this scenario.
	rootDomainName := dnsRecord.GetSpec().RootHost
	zoneDomainFilter := externaldnsendpoint.NewDomainFilter([]string{dnsRecord.GetZoneDomainName()})
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}
	var excludeDNSRecordTypes []string

	registry, err := externaldnsregistry.NewTXTRegistry(ctx, dnsProvider, txtRegistryPrefix, txtRegistrySuffix,
		dnsRecord.GetOwnerID(), txtRegistryCacheInterval, txtRegistryWildcardReplacement, managedDNSRecordTypes,
		excludeDNSRecordTypes, txtRegistryEncryptEnabled, []byte(txtRegistryEncryptAESKey))
	if err != nil {
		return false, err
	}

	policyID := "sync"
	policy, exists := externaldnsplan.Policies[policyID]
	if !exists {
		return false, fmt.Errorf("unknown policy: %s", policyID)
	}

	specEndpoints := dnsRecord.GetEndpoints()

	//If we are deleting set the expected endpoints to an empty array
	if isDelete {
		specEndpoints = []*externaldnsendpoint.Endpoint{}
	}

	//zoneEndpoints = Records in the current dns provider zone
	zoneEndpoints, err := registry.Records(ctx)
	if err != nil {
		return false, err
	}

	//specEndpoints = Records that this DNSRecord expects to exist
	specEndpoints, err = registry.AdjustEndpoints(specEndpoints)
	if err != nil {
		return false, fmt.Errorf("adjusting specEndpoints: %w", err)
	}

	//statusEndpoints = Records that were created/updated by this DNSRecord last
	statusEndpoints, err := registry.AdjustEndpoints(dnsRecord.GetStatus().Endpoints)
	if err != nil {
		return false, fmt.Errorf("adjusting statusEndpoints: %w", err)
	}

	//Note: All endpoint lists should be in the same provider specific format at this point
	logger.V(1).Info("applyChanges", "zoneEndpoints", zoneEndpoints,
		"specEndpoints", specEndpoints, "statusEndpoints", statusEndpoints)

	plan := externaldnsplan.NewPlan(ctx, zoneEndpoints, statusEndpoints, specEndpoints, []externaldnsplan.Policy{policy},
		externaldnsendpoint.MatchAllDomainFilters{&zoneDomainFilter}, managedDNSRecordTypes, excludeDNSRecordTypes,
		registry.OwnerID(), &rootDomainName,
	)

	plan = plan.Calculate()
	if err = plan.Error(); err != nil {
		return false, err
	}
	dnsRecord.SetStatusDomainOwners(plan.Owners)
	dnsRecord.SetStatusEndpoints(specEndpoints)
	if plan.Changes.HasChanges() {
		//ToDo (mnairn) CoreDNS will always think it has changes as long as provider.Records() returns an empty slice
		// Figure out a better way of doing this that avoids the check for a specific provider here
		hasChanges := dnsProvider.Name() != provider.DNSProviderCoreDNS
		logger.Info("Applying changes")
		err = registry.ApplyChanges(ctx, plan.Changes)
		return hasChanges, err
	}
	return false, nil
}

func (r *BaseDNSRecordReconciler) updateStatus(ctx context.Context, client client.Client, previous, current DNSRecordAccessor, err error) (reconcile.Result, error) {
	result, uErr := r.updateStatusAndRequeue(ctx, client, previous, current, 0)
	if uErr != nil {
		err = uErr
	}
	return result, err
}

// updateStatusAndRequeue will update the status of the record if the current and previous status is different
// and returns a reconcile.result that re-queues at the given time.
func (r *BaseDNSRecordReconciler) updateStatusAndRequeue(ctx context.Context, client client.Client, previous, current DNSRecordAccessor, requeueTime time.Duration) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	// update the record after setting the status
	if !equality.Semantic.DeepEqual(previous.GetStatus(), current.GetStatus()) {
		logger.V(1).Info("Updating status of DNSRecord")
		if updateError := client.Status().Update(ctx, current.GetDNSRecord()); updateError != nil {
			if apierrors.IsConflict(updateError) {
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			return ctrl.Result{}, updateError
		}
	}
	logger.V(1).Info(fmt.Sprintf("Requeue in %s", requeueTime.String()))

	return ctrl.Result{RequeueAfter: requeueTime}, nil
}
