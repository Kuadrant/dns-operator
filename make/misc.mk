
##@ Misc

## Miscellaneous targets

.PHONY: install-metallb
install-metallb: SUBNET_OFFSET=1
install-metallb: CIDR=28
install-metallb: NUM_IPS=16
install-metallb: yq ## Install the metallb load balancer allowing use of a LoadBalancer type service
	kubectl apply --server-side -k config/metallb
	kubectl -n metallb-system wait --for=condition=Available=True deployments controller --timeout=300s
	kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=60s
	curl -s https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/utils/docker-network-ipaddresspool.sh | bash -s kind $(YQ) ${SUBNET_OFFSET} ${CIDR} ${NUM_IPS} | kubectl apply -n metallb-system -f -

.PHONY: install-observability
install-observability: ## Install the kuadrant observability stack
	kubectl apply --server-side -k config/observability || true
	kubectl apply --server-side -k config/observability # Run twice if it fails the first time due to CRDs
	kubectl -n monitoring wait --timeout=60s --for=condition=Available=True deployments --all

.PHONY: install-coredns
install-coredns: COREDNS_KUSTOMIZATION=config/coredns
install-coredns: kustomize ## Install CoreDNS
	${KUSTOMIZE} build --enable-helm ${COREDNS_KUSTOMIZATION} | kubectl apply -f -
	kubectl wait --timeout=90s --for=condition=Ready=True pods -A -l app.kubernetes.io/name=coredns

.PHONY: install-coredns-unmonitored
install-coredns-unmonitored: kustomize ## Install CoreDNS without ServiceMonitor
	${MAKE} install-coredns COREDNS_KUSTOMIZATION=config/coredns-unmonitored

.PHONY: install-coredns-multi
install-coredns-multi: kustomize ## Install CoreDNS Multi POC Setup
	${MAKE} install-coredns COREDNS_KUSTOMIZATION=config/coredns-multi

.PHONY: install-bind9
install-bind9: BIND9_KUSTOMIZATION=config/bind9
install-bind9: kustomize ## Install Bind9
	${KUSTOMIZE} build ${BIND9_KUSTOMIZATION} | kubectl apply -f -
	kubectl wait --timeout=90s --for=condition=Ready=True pods -A -l app.kubernetes.io/name=bind9

.PHONY: delete-bind9
delete-bind9: BIND9_KUSTOMIZATION=config/bind9
delete-bind9: kustomize ## Install Bind9
	${KUSTOMIZE} build ${BIND9_KUSTOMIZATION} | kubectl delete -f -

.PHONY: multicluster-local-setup
multicluster-local-setup: CLUSTER_COUNT=2
multicluster-local-setup: ## Opinionated multi cluster local development setup
	@echo "multicluster-local-setup: CLUSTER_COUNT=${CLUSTER_COUNT}"
	@if [ ${CLUSTER_COUNT} -le 1 ] ; then \
  		echo "multicluster-local-setup: error: CLUSTER_COUNT must be greater than one!!" ;\
		exit 1 ;\
	fi
	$(MAKE) -s local-setup CLUSTER_COUNT=${CLUSTER_COUNT} DEPLOY=true
	@n=2 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
		$(MAKE) -s kubeconfig-secret-create-kind-internal NAMESPACE=dns-operator-system NAME=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n TARGET_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-1 REMOTE_CONTEXT=kind-${KIND_CLUSTER_NAME_PREFIX}-$$n SERVICE_ACCOUNT=dns-operator-remote-cluster ;\
		((n = n + 1)) ;\
	done
	@echo "multicluster-local-setup: listing cluster secrets:"
	kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-${KIND_CLUSTER_NAME_PREFIX}-1
	@echo "multicluster-local-setup: tail cluster logs using the following commands:"
	@n=1 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
  		echo "   kubectl stern deployment/dns-operator-controller-manager -n dns-operator-system --context kind-${KIND_CLUSTER_NAME_PREFIX}-$$n" ;\
		((n = n + 1)) ;\
	done
	@echo "multicluster-local-setup: setup complete!"
