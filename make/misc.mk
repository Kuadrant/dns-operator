
##@ Misc

## Miscellaneous targets

.PHONY: install-metallb
install-metallb: SUBNET_OFFSET=1
install-metallb: yq ## Install the metallb load balancer allowing use of a LoadBalancer type service
	kubectl apply --server-side -k config/metallb
	kubectl -n metallb-system wait --for=condition=Available=True deployments controller --timeout=300s
	kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=60s
	curl -s https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/utils/docker-network-ipaddresspool.sh | bash -s kind $(YQ) ${SUBNET_OFFSET} | kubectl apply -n metallb-system -f -

.PHONY: install-observability
install-observability: ## Install the kuadrant observability stack
	kubectl apply --server-side -k config/observability || true
	kubectl apply --server-side -k config/observability # Run twice if it fails the first time due to CRDs
	kubectl -n monitoring wait --timeout=60s --for=condition=Available=True deployments --all

.PHONY: install-coredns
install-coredns: kustomize ## Install CoreDNS
	${KUSTOMIZE} build --enable-helm config/coredns/ | kubectl apply -f -
	kubectl -n kuadrant-dns wait --timeout=60s --for=condition=Available=True deployments --all
