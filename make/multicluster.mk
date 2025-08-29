
##@ MultiCluster

## Multi cluster related helper targets
##
## Create multicluster dev/test environments
##
## e.g. make multicluster-local-setup CLUSTER_COUNT=2 PRIMARY_CLUSTER_COUNT=1
##
## CLUSTER_COUNT = Total kind clusters to create
## PRIMARY_CLUSTER_COUNT = Number of clusters that should be primary
## CLUSTER_COUNT - PRIMARY_CLUSTER_COUNT = the number of secondary clusters
## DEPLOY = Deploy the dns operator on each cluster true|false
##
## Examples:
##   1 primary and 1 secondary (default):
##     multicluster-local-setup CLUSTER_COUNT=2 PRIMARY_CLUSTER_COUNT=1
##   2 primaries:
##     multicluster-local-setup CLUSTER_COUNT=2 PRIMARY_CLUSTER_COUNT=2
##   2 primary and 1 secondary:
##     multicluster-local-setup CLUSTER_COUNT=3 PRIMARY_CLUSTER_COUNT=2
##
## primary cluster:
##   - metallb installed
##   - bind9 installed
##   - coredns + kuadrant plugin installed
##   - dns-operator-system namespace:
##     - running dns-operator (delegation-role=primary) if DEPLOY=true
##     - kubeconfig secrets for every other cluster
##   - dnstest namespace:
##     - dns provider secret for inmem provider
##     - dns provider secret for coredns (configured for the current set of clusters)
##     - dns provider secrets for external providers (aws, gcp, azure etc..) depending on local config
##
## secondary cluster:
##   - metallb installed
##   - dns-operator-system namespace:
##     - running dns-operator(delegation-role=secondary) if DEPLOY=true
##

.PHONY: multicluster-local-setup
multicluster-local-setup: CLUSTER_COUNT=2
multicluster-local-setup: PRIMARY_CLUSTER_COUNT=1
multicluster-local-setup: DEPLOY=true
multicluster-local-setup: ## Create multi cluster local development/test setups with primary and secondary clusters
	@echo "multicluster-local-setup: CLUSTER_COUNT=${CLUSTER_COUNT} PRIMARY_CLUSTER_COUNT=${PRIMARY_CLUSTER_COUNT} DEPLOY=${DEPLOY}"
	@if [ ${CLUSTER_COUNT} -le 1 ] ; then \
  		echo "multicluster-local-setup: error: CLUSTER_COUNT must be greater than one!!" ;\
		exit 1 ;\
	fi
	@if [ ${PRIMARY_CLUSTER_COUNT} -lt 1 ] ; then \
		echo "multicluster-local-setup: error: PRIMARY_CLUSTER_COUNT must be at least one!!" ;\
		exit 1 ;\
    fi
	@if [ ${CLUSTER_COUNT} -lt ${PRIMARY_CLUSTER_COUNT} ] ; then \
		echo "multicluster-local-setup: error: PRIMARY_CLUSTER_COUNT must be less than CLUSTER_COUNT!!" ;\
		exit 1 ;\
    fi

	@echo "multicluster-local-setup: creating kind clusters"
	@n=1 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
		if [ $$n -le ${PRIMARY_CLUSTER_COUNT} ]; then\
			echo "multicluster-local-setup: creating primary cluster ${KIND_CLUSTER_NAME_PREFIX}-$$n" ;\
  			$(MAKE) -s local-setup-cluster KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME_PREFIX}-$$n SUBNET_OFFSET=$$n DELEGATION_ROLE=primary DEPLOY=${DEPLOY} ;\
		else\
			echo "multicluster-local-setup: creating secondary cluster ${KIND_CLUSTER_NAME_PREFIX}-$$n" ;\
  			$(MAKE) -s local-setup-cluster KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME_PREFIX}-$$n SUBNET_OFFSET=$$n DELEGATION_ROLE=secondary DEPLOY=${DEPLOY} ;\
		fi ;\
		((n = n + 1)) ;\
	done

	@echo "multicluster-local-setup: generate coredns config and add dns providers to primary clusters"
	$(MAKE) local-setup-coredns-generate-from-clusters
	@n=1 ; while [[ $$n -le $(PRIMARY_CLUSTER_COUNT) ]] ; do \
		$(MAKE) -s local-setup-cluster-dns-providers KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME_PREFIX}-$$n ;\
		((n = n + 1)) ;\
	done ;\

	@echo "multicluster-local-setup: create kubeconfig secrets for the current set of primary clusters"
	$(MAKE) multicluster-local-setup-create-kubeconfig-secrets CLUSTER_COUNT=${CLUSTER_COUNT} PRIMARY_CLUSTER_COUNT=${PRIMARY_CLUSTER_COUNT} DEPLOY=${DEPLOY}

	@echo "multicluster-local-setup: tail cluster logs using the following commands:"
	@n=1 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
  		echo "   kubectl stern deployment/dns-operator-controller-manager -n dns-operator-system --context kind-${KIND_CLUSTER_NAME_PREFIX}-$$n" ;\
		((n = n + 1)) ;\
	done

	kubectl config use-context kind-${KIND_CLUSTER_NAME_PREFIX}-1
	@echo "multicluster-local-setup: Complete!!"

.PHONY: multicluster-local-setup-create-kubeconfig-secrets
multicluster-local-setup-create-kubeconfig-secrets: CLUSTER_COUNT=2
multicluster-local-setup-create-kubeconfig-secrets: PRIMARY_CLUSTER_COUNT=1
multicluster-local-setup-create-kubeconfig-secrets: DEPLOY=true
multicluster-local-setup-create-kubeconfig-secrets: ## Create kubeconfig secrets on each primary for all other clusters
	@n=1 ; while [[ $$n -le $(PRIMARY_CLUSTER_COUNT) ]] ; do \
		c=1 ; while [[ $$c -le $(CLUSTER_COUNT) ]] ; do \
			if [ $$n != $$c ]; then\
				echo "adding kubeconfig secret for kind-${KIND_CLUSTER_NAME_PREFIX}-$$c to kind-${KIND_CLUSTER_NAME_PREFIX}-$$n" ;\
				if [ ${DEPLOY} = "true" ]; then\
					$(MAKE) -s kubeconfig-secret-create-kind-internal NAMESPACE=dns-operator-system NAME=kind-${KIND_CLUSTER_NAME_PREFIX}-$$c TARGET_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n REMOTE_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$c SERVICE_ACCOUNT=dns-operator-remote-cluster ;\
				else \
					$(MAKE) -s kubeconfig-secret-create NAMESPACE=dns-operator-system NAME=kind-${KIND_CLUSTER_NAME_PREFIX}-$$c TARGET_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n REMOTE_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$c SERVICE_ACCOUNT=dns-operator-remote-cluster ;\
				fi ;\
			fi ;\
			((c = c + 1)) ;\
		done ;\
		((n = n + 1)) ;\
	done ;\

.PHONY: multicluster-local-setup-delete-kubeconfig-secrets
multicluster-local-setup-delete-kubeconfig-secrets: CLUSTER_COUNT=2
multicluster-local-setup-delete-kubeconfig-secrets: ## Delete all kubeconfig secrets from all clusters
	@c=1 ; while [[ $$c -le $(CLUSTER_COUNT) ]] ; do \
  		$(KUBECTL) config use-context kind-${KIND_CLUSTER_NAME_PREFIX}-$$c ;\
		$(MAKE) -s kubeconfig-secret-delete-all ;\
		((c = c + 1)) ;\
	done ;\
