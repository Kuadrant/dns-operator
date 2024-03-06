
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${SCRIPT_DIR}/../bin"

KUSTOMIZE_BIN="${BIN_DIR}/kustomize"
KIND_BIN="${BIN_DIR}/kind"

TEST_NS="mntest"

CLUSTER_PREFIX="${CLUSTER_PREFIX:-kuadrant-dns-local}"
CLUSTER_COUNT="${CLUSTER_COUNT:-1}"

function prepend() { while read line; do echo "${1}${line}"; done; }

cleanClusters() {
	# Delete existing kind clusters
	clusterCount=$(${KIND_BIN} get clusters | grep ${CLUSTER_PREFIX} | wc -l)
	if ! [[ $clusterCount =~ "0" ]] ; then
		echo "Deleting previous clusters."
		${KIND_BIN} get clusters | grep ${CLUSTER_PREFIX} | xargs ${KIND_BIN} delete clusters
	fi
}

localDeploy() {
  clusterName=${1}
  make local-deploy KIND_CLUSTER_NAME=${clusterName}

  kubectl config use-context kind-${clusterName}

  kubectl -n dns-operator-system wait --timeout=60s --for=condition=Available deployments --all
  kubectl get deployments -n dns-operator-system

  kubectl create namespace ${TEST_NS} --dry-run=client -o yaml | kubectl apply -f -

  if [[ -f "config/local-setup/managedzone/gcp/managed-zone-config.env" && -f "config/local-setup/managedzone/gcp/gcp-credentials.env" ]]; then
    ${KUSTOMIZE_BIN} build config/local-setup/managedzone/gcp | kubectl -n ${TEST_NS} apply -f -
  fi
  if [[ -f "config/local-setup/managedzone/aws/managed-zone-config.env" && -f "config/local-setup/managedzone/aws/aws-credentials.env" ]]; then
    ${KUSTOMIZE_BIN} build config/local-setup/managedzone/aws | kubectl -n ${TEST_NS} apply  -f -
  fi
  kubectl get managedzones -n ${TEST_NS}
}

applyDNSRecords() {
  kubectl -n ${TEST_NS} apply -f "${1}"
  kubectl -n ${TEST_NS} wait --timeout=10s --for=condition=Ready dnsrecord --all
  kubectl get dnsrecords -n ${TEST_NS}
}

deleteDNSRecords() {
  kubectl -n ${TEST_NS} delete -f "${1}"
  kubectl -n ${TEST_NS} wait --for delete dnsrecord --all
  kubectl get dnsrecords -n ${TEST_NS}
}

testDNSRecords() {
  applyDNSRecords "${1}"
  read -p "Check record create status. Press enter to continue"
  deleteDNSRecords "${1}"
  read -p "Check record delete status. Press enter to continue"
}

## --- Cluster Setup Start --- ##

make kind kustomize
cleanClusters

for ((i = 1; i <= ${CLUSTER_COUNT}; i++)); do
  clusterName=${CLUSTER_PREFIX}-${i}
  echo "Creating cluster ${i}/${CLUSTER_COUNT}: ${clusterName}"
  make local-setup KIND_CLUSTER_NAME=${clusterName} | prepend "[${clusterName}] "
  localDeploy ${clusterName} | prepend "[${clusterName}] "
done

## --- Cluster Setup End --- ##

# If running controller locally remove ths deployment first
#kubectl delete deployments dns-operator-controller-manager -n dns-operator-system


## --- DNSRecord Test Start --- ##

#Simple (Single Cluster)
printf "\n\n ### Simple (Single Cluster) ###\n\n"

testDNSRecords "${SCRIPT_DIR}/dnsrecords/simple/dnsrecord-*cluster1.yaml"
testDNSRecords "${SCRIPT_DIR}/dnsrecords/simple/dnsrecord-*cluster2.yaml"
testDNSRecords "${SCRIPT_DIR}/dnsrecords/simple/dnsrecord-*cluster3.yaml"

##LoadBalanced (Single Cluster)
printf "\n\n ### LoadBalanced (Single Cluster) ###\n\n"

testDNSRecords "${SCRIPT_DIR}/dnsrecords/loadbalanced/dnsrecord-*cluster1.yaml"
testDNSRecords "${SCRIPT_DIR}/dnsrecords/loadbalanced/dnsrecord-*cluster2.yaml"
testDNSRecords "${SCRIPT_DIR}/dnsrecords/loadbalanced/dnsrecord-*cluster3.yaml"

##Simple (Multi Cluster with control plane)
printf "\n\n ### Simple (Multi Cluster with control plane) ###\n\n"

testDNSRecords "${SCRIPT_DIR}/dnsrecords/simple/combined"

##LoadBalanced (Multi Cluster with control plane)
printf "\n\n ### LoadBalanced (Multi Cluster with control plane) ###\n\n"

testDNSRecords "${SCRIPT_DIR}/dnsrecords/loadbalanced/combined"

#Simple (Multi Cluster no control plane)
printf "\n\n ### Simple (Multi Cluster no control plane) ###\n\n"

testDNSRecords "${SCRIPT_DIR}/dnsrecords/simple"

#LoadBalanced (Multi Cluster no control plane)
printf "\n\n ### LoadBalanced (Multi Cluster no control plane) ###\n\n"

testDNSRecords "${SCRIPT_DIR}/dnsrecords/loadbalanced"

#Conflicting strategies (simple and loadbalanced)

#Google only
#testDNSRecords "${SCRIPT_DIR}/dnsrecords/conflicting/dnsrecord-*google*.yaml"

## --- DNSRecord Test End --- ##
