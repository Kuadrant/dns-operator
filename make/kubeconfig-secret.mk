
##@ Kubeconfig Secrets

## Targets to help manage cluster kubeconfig secrets

.PHONY: kubeconfig-secret-create
kubeconfig-secret-create: NAMESPACE=dns-operator-system
kubeconfig-secret-create: NAME=kind-kuadrant-dns-local-2
kubeconfig-secret-create: TARGET_CONTEXT=kind-kuadrant-dns-local-1
kubeconfig-secret-create: REMOTE_CONTEXT=kind-kuadrant-dns-local-2
kubeconfig-secret-create: SERVICE_ACCOUNT=dns-operator-remote-cluster
kubeconfig-secret-create: ## Create a kubeconfig (cluster) secret on a target "primary" cluster to access a "remote" cluster
	kubectl config use-context $(TARGET_CONTEXT)
	hack/create-kubeconfig-secret.sh -c $(REMOTE_CONTEXT) -a $(SERVICE_ACCOUNT) -n $(NAMESPACE) --name $(NAME)

# When using kind and deployed operators, cluster secrets need to be created with the correct server url in order for communication to be established correctly.
.PHONY: kubeconfig-secret-create-kind-internal
kubeconfig-secret-create-kind-internal: NAMESPACE=dns-operator-system
kubeconfig-secret-create-kind-internal: NAME=kind-kuadrant-dns-local-2
kubeconfig-secret-create-kind-internal: TARGET_CONTEXT=kind-kuadrant-dns-local-1
kubeconfig-secret-create-kind-internal: REMOTE_CONTEXT=kind-kuadrant-dns-local-2
kubeconfig-secret-create-kind-internal: SERVICE_ACCOUNT=dns-operator-controller-manager
kubeconfig-secret-create-kind-internal: ## Create a kubeconfig secret (cluster) on a target "primary" cluster to access a "remote" cluster.
	kubectl config use-context $(TARGET_CONTEXT) --kubeconfig tmp/kubeconfigs/kuadrant-local-all.internal.kubeconfig
	docker run --rm \
		-u $${UID} \
		-v $(shell pwd):/tmp/dns-operator:z \
		--network kind \
		-e KUBECONFIG=/tmp/dns-operator/tmp/kubeconfigs/kuadrant-local-all.internal.kubeconfig alpine/k8s:1.30.13 \
		/tmp/dns-operator/hack/create-kubeconfig-secret.sh -c $(REMOTE_CONTEXT) -a $(SERVICE_ACCOUNT) -n $(NAMESPACE) --name $(NAME)

.PHONY: kubeconfig-secret-delete
kubeconfig-secret-delete: NAMESPACE=dns-operator-system
kubeconfig-secret-delete: NAME=kind-kuadrant-dns-local-2
kubeconfig-secret-delete:  ## Delete a kubeconfig (cluster) secret from the current cluster.
	kubectl delete secret $(NAME) -n $(NAMESPACE)

.PHONY: kubeconfig-secret-delete-all
kubeconfig-secret-delete-all: ## Delete all kubeconfig (cluster) secrets from the current cluster.
	kubectl delete secret -A -l kuadrant.io/multicluster-kubeconfig=true
