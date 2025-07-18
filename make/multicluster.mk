
##@ MultiCluster

## Multi cluster related helper targets

.PHONY: multicluster-local-setup
multicluster-local-setup: CLUSTER_COUNT=2
multicluster-local-setup: DEPLOY=true
multicluster-local-setup: ## Opinionated multi cluster local development/test setup
	@echo "multicluster-local-setup: CLUSTER_COUNT=${CLUSTER_COUNT}"
	@if [ ${CLUSTER_COUNT} -le 1 ] ; then \
  		echo "multicluster-local-setup: error: CLUSTER_COUNT must be greater than one!!" ;\
		exit 1 ;\
	fi
	$(MAKE) -s local-setup CLUSTER_COUNT=${CLUSTER_COUNT} DEPLOY=${DEPLOY}
	@n=2 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
		if [ ${DEPLOY} = "true" ]; then\
  			$(MAKE) -s kubeconfig-secret-create-kind-internal NAMESPACE=dns-operator-system NAME=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n TARGET_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-1 REMOTE_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n SERVICE_ACCOUNT=dns-operator-remote-cluster ;\
		else\
  			$(MAKE) -s kubeconfig-secret-create NAMESPACE=dns-operator-system NAME=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n TARGET_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-1 REMOTE_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n SERVICE_ACCOUNT=dns-operator-remote-cluster ;\
		fi ;\
		((n = n + 1)) ;\
	done
	@echo "multicluster-local-setup: listing cluster secrets:"
	kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-${KIND_CLUSTER_NAME_PREFIX}-1
	@echo "multicluster-local-setup: tail cluster logs using the following commands:"
	@n=1 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
  		echo "   kubectl stern deployment/dns-operator-controller-manager -n dns-operator-system --context kind-${KIND_CLUSTER_NAME_PREFIX}-$$n" ;\
		((n = n + 1)) ;\
	done
	kubectl config use-context kind-${KIND_CLUSTER_NAME_PREFIX}-1
	@echo "multicluster-local-setup: setup complete!"
