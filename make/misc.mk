
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

.PHONY: create-kubeconfig-secret
create-kubeconfig-secret: NAMESPACE=dns-operator-system
create-kubeconfig-secret: NAME=kind-kuadrant-dns-local-2
create-kubeconfig-secret: TARGET_CONTEXT=kind-kuadrant-dns-local-1
create-kubeconfig-secret: REMOTE_CONTEXT=kind-kuadrant-dns-local-2
create-kubeconfig-secret: SERVICE_ACCOUNT=dns-operator-controller-manager
create-kubeconfig-secret: ## Create a kubeconfig (cluster) secret on a target "primary" cluster to access a "remote" cluster
	kubectl config use-context $(TARGET_CONTEXT)
	hack/create-kubeconfig-secret.sh -c $(REMOTE_CONTEXT) -a $(SERVICE_ACCOUNT) -n $(NAMESPACE) --name $(NAME)

# When using kind and deployed operators, cluster secrets need to be created with the correct server url in order for communication to be established correctly.
.PHONY: create-kubeconfig-secret-kind-internal
create-kubeconfig-secret-kind-internal: NAMESPACE=dns-operator-system
create-kubeconfig-secret-kind-internal: NAME=kind-kuadrant-dns-local-2
create-kubeconfig-secret-kind-internal: TARGET_CONTEXT=kind-kuadrant-dns-local-1
create-kubeconfig-secret-kind-internal: REMOTE_CONTEXT=kind-kuadrant-dns-local-2
create-kubeconfig-secret-kind-internal: SERVICE_ACCOUNT=dns-operator-controller-manager
create-kubeconfig-secret-kind-internal: ## Create a kubeconfig secret (cluster) on a target "primary" cluster to access a "remote" cluster.
	kubectl config use-context $(TARGET_CONTEXT) --kubeconfig tmp/kubeconfigs/kuadrant-local-all.internal.kubeconfig
	docker run --rm \
		-u $${UID} \
		-v $(shell pwd):/tmp/dns-operator:z \
		--network kind \
		-e KUBECONFIG=/tmp/dns-operator/tmp/kubeconfigs/kuadrant-local-all.internal.kubeconfig alpine/k8s:1.30.13 \
		/tmp/dns-operator/hack/create-kubeconfig-secret.sh -c $(REMOTE_CONTEXT) -a $(SERVICE_ACCOUNT) -n $(NAMESPACE) --name $(NAME)

.PHONY: delete-kubeconfig-secret
delete-kubeconfig-secret: NAMESPACE=dns-operator-system
delete-kubeconfig-secret: NAME=kind-kuadrant-dns-local-2
delete-kubeconfig-secret:  ## Delete a kubeconfig (cluster) secret from the current cluster.
	kubectl delete secret $(NAME) -n $(NAMESPACE)

.PHONY: delete-all-kubeconfig-secrets
delete-all-kubeconfig-secrets: ## Delete all kubeconfig (cluster) secrets from the current cluster.
	kubectl delete secret -A -l kuadrant.io/multicluster-kubeconfig=true
