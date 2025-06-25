# Copyright 2024 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

REPO_ROOT:=${CURDIR}
OUT_DIR=$(REPO_ROOT)/bin

# disable CGO by default for static binaries
CGO_ENABLED=0
export GOROOT GO111MODULE CGO_ENABLED

# Setting SHELL to bash allows bash commands to be executed by recipes.                                                                            # Options are set to exit when a recipe line exits non-zero or a piped command fails.                                                                                                                             
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

default: build ## Default builds

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


build: build-dracpu ## build dracpu

build-dracpu: ## build dracpu
	go build -v -o "$(OUT_DIR)/dracpu" ./cmd/dracpu


clean: ## clean
	rm -rf "$(OUT_DIR)/"

test: ## run tests
	CGO_ENABLED=1 go test -v -race -count 1 ./...

update: ## runs go mod tidy
	go mod tidy

# get image name from directory we're building
IMAGE_NAME=dracputest
# docker image registry, default to upstream
REGISTRY?=gcr.io/gke-networking-test-images
# tag based on date-sha
TAG?=$(shell echo "$$(date +v%Y%m%d)-$$(git describe --always --dirty)")
# the full image tag
IMAGE?=$(REGISTRY)/$(IMAGE_NAME):$(TAG)
PLATFORMS?=linux/arm64

# required to enable buildx
export DOCKER_CLI_EXPERIMENTAL=enabled
image: ## docker build load
# docker buildx build --platform=${PLATFORMS} $(OUTPUT) --progress=$(PROGRESS) -t ${IMAGE} --pull $(EXTRA_BUILD_OPT) .
	docker build . -t ${IMAGE} --load

image-build: ## build image
	docker buildx build . \
		--platform="${PLATFORMS}" \
		--tag="${IMAGE}"

push-image: image ## build and push image
	docker tag ${IMAGE} gcr.io/gke-networking-test-images/dracputest:stable
	docker push ${IMAGE}
	docker push gcr.io/gke-networking-test-images/dracputest:stable

kind-cluster:  ## create kind cluster
	kind create cluster --name dra --config kind.yaml

load-kind-image: image ## install on cluster
	docker tag ${IMAGE} ghcr.io/google/dracpu:stable
	kind load docker-image ghcr.io/google/dracpu:stable --name dra
	kubectl delete -f install.yaml || true
	kubectl apply -f install.yaml

delete-kind-cluster: ## delete kind cluster
	kind create cluster --name dra
