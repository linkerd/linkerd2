# Cluster management tools.

export CLUSTER_NAME ?= $(shell cat tmp/current 2>/dev/null || echo $$(whoami)-dev)
export MACHINE_TYPE ?= n1-standard-2
export DISK_SIZE ?= 100
export MAX_NODES ?= 10
export NETWORK ?= dev
export PROJECT ?= $(shell gcloud config get-value project)
export VERSION ?= latest
export ZONE ?= us-west1-a
GKE_DASHBOARD = http://localhost:8001/api/v1/namespaces/kube-system/services/https:kubernetes-dashboard:/proxy/

define gcloud_container
	gcloud beta container \
		--project "$(PROJECT)" \
		clusters \
		--zone "$(ZONE)"
endef

define gcloud_compute
	gcloud compute --project=$(PROJECT)
endef

tmp:
	mkdir tmp

HAS_GCLOUD := $(shell command -v gcloud;)
HAS_HELM := $(shell command -v helm;)
HAS_KUBECTL := $(shell command -v kubectl;)
HAS_AZ := $(shell command -v az;)
HAS_PULUMI := $(shell command -v pulumi;)

.PHONY: bootstrap
bootstrap:
	@# Bootstrap the local required binaries
ifndef HAS_GCLOUD
	curl https://sdk.cloud.google.com | bash
endif
ifndef HAS_KUBECTL
	gcloud components install kubectl
endif
ifndef HAS_HELM
	curl https://raw.githubusercontent.com/kubernetes/helm/master/scripts/get | bash
endif
ifndef HAS_PULUMI
	curl -fsSL https://get.pulumi.com | sh
endif


.PHONY: create
create: bootstrap
	@# Create a cluster in GKE with some sane defaults.
	@# Options:
	@#
	@#     CLUSTER_NAME                       :: ${CLUSTER_NAME}
	@#     MACHINE_TYPE                       :: ${MACHINE_TYPE}
	@#     MAX_NODES                          :: ${MAX_NODES}
	@#     NETWORK                            :: ${NETWORK}
	@#     PROJECT                            :: ${PROJECT}
	@#     VERSION                            :: ${VERSION}
	@#     ZONE                               :: ${ZONE}
	cd gcp-cluster; \
	npm install; \
	pulumi config set gcp:project ${PROJECT}; \
	pulumi config set gcp:zone ${ZONE}; \
	pulumi up;
	 $(MAKE) get-auth set-current run-proxy
	 kubectl create clusterrolebinding \
	 	$$(whoami)-cluster-admin \
	 	--clusterrole=cluster-admin \
	 	--user=$$(gcloud config get-value account)
	 kubectl apply -f psp.yaml -f rbac.yaml -f tiller.yaml
	 helm init --service-account tiller
	 $(MAKE) show-dashboard

.PHONY: delete
delete:
	@# Deletes the current cluster.
	@# Options:
	@#
	@#     CLUSTER_NAME                       :: ${CLUSTER_NAME}
	@#     PROJECT                            :: ${PROJECT}
	@#     ZONE                               :: ${ZONE}
	cd gcp-cluster; \
	pulumi destroy;
.PHONY: get-auth
get-auth:
	@# Configure kubectl to connect to remote cluster.
	@# Options:
	@#
	@#     CLUSTER_NAME                       :: ${CLUSTER_NAME}
	$(call gcloud_container) \
		get-credentials \
		$(CLUSTER_NAME)
	kubectl config delete-context gke-$(CLUSTER_NAME) || true
	kubectl config rename-context \
		$$(kubectl config current-context) \
		gke-$(CLUSTER_NAME)

.PHONY: set-current
set-current:
	@# Set the current cluster and setup kubectl to use it.
	@# Options:
	@#
	@#     CLUSTER_NAME                       :: ${CLUSTER_NAME}
	kubectl config use-context gke-$(CLUSTER_NAME)

.PHONY: run-proxy
run-proxy: tmp
	@# Run the proxy so that the dashboard is accessible.
	lsof -i4TCP:8001 -sTCP:LISTEN -t | xargs kill
	kubectl proxy >tmp/proxy.log 2>&1 &

.PHONY: create-network
create-network:
	@# Create a VPC network for use by k8s clusters. Allow current IP by default.
	@# Options:
	@#
	@#     NETWORK                            :: ${NETWORK}
	@#     PROJECT                            :: ${PROJECT}
	@#     ZONE                               :: ${ZONE}
	cd gcp-network; \
	npm install; \
	pulumi config set gcp:project ${PROJECT}; \
	pulumi config set gcp:zone ${ZONE}; \
	pulumi up;

.PHONY: show-dashboard
show-dashboard:
	@# Show the URL that can be used to access the dashboard
	@echo "Go to $(GKE_DASHBOARD) for the dashboard. Note: RBAC is permissive for the dashboard, no need to enter a token."



.PHONY: help
help: SHELL := /bin/bash
help:
	@# Output all targets available.
	@ echo "usage: make [target] ..."
	@ echo ""
	@eval "echo \"$$(grep -h -B1 $$'^\t@#' $(MAKEFILE_LIST) \
		| sed 's/@#//' \
		| awk \
			-v NO_COLOR="$(NO_COLOR)" \
			-v OK_COLOR="$(OK_COLOR)" \
			-v RS="--\n" \
			-v FS="\n" \
			-v OFS="@@" \
			'{ split($$1,target,":"); $$1=""; print OK_COLOR target[1] NO_COLOR $$0 }' \
		| sort \
		| awk \
			-v FS="@@" \
			-v OFS="\n" \
			'{ CMD=$$1; $$1=""; print CMD $$0 }')\""

.DEFAULT_GOAL := help
