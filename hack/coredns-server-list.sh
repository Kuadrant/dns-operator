#!/bin/bash

# Queries currently running clusters for coredns instances and returns a comma seperated string of their addresses.
#
# Example:
# ./hack/coredns-server-list.sh kind-kuadrant-dns-local 2
# 172.18.0.16:53,172.18.0.17:53,172.18.0.32:53,172.18.0.33:53

set -euo pipefail

CONTEXT=${1}
CLUSTER_COUNT=${2:-1}
labels=${3:-app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics}

ns=()
for i in $(seq $CLUSTER_COUNT); do
  ns+=("$(kubectl --context ${CONTEXT}-${i} get service -A -l ${labels} -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip + ":53")] | join(",")')")
done
echo "$(IFS=, ; echo "${ns[*]}")"
