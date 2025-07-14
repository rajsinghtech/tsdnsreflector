.PHONY: build test clean lint docker docker-setup docker-multiarch run release ci-test ci-docker k8s-deploy k8s-undeploy k8s-secret k8s-logs k8s-status

# Variables
BINARY_NAME=tsdnsreflector
BUILD_DIR=.
GO_VERSION=1.24
REGISTRY=ghcr.io/rajsinghtech
IMAGE_NAME=tsdnsreflector
TAG?=latest
NAMESPACE=tsdnsreflector
AUTHKEY?=

# Build
build:
	go build -o $(BINARY_NAME) ./cmd/tsdnsreflector

# Test
test:
	go test -short -race -coverprofile=coverage.out ./... 2>&1 | grep -v "ld: warning.*LC_DYSYMTAB" || true

# Test with full integration/container tests
test-full:
	go test -v -race -coverprofile=coverage.out ./... 2>&1 | grep -v "ld: warning.*LC_DYSYMTAB" || true

# Lint
lint:
	golangci-lint run

# Clean
clean:
	rm -f $(BINARY_NAME) coverage.out

# Docker
docker:
	docker build -t $(BINARY_NAME) .

# Docker buildx setup (run once)
docker-setup:
	docker buildx create --name multiarch --driver docker-container --bootstrap || true
	docker buildx use multiarch

# Build multi-arch Docker images for ARM64 and AMD64
docker-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 \
		-t $(REGISTRY)/$(IMAGE_NAME):$(TAG) \
		-t $(REGISTRY)/$(IMAGE_NAME):latest \
		--push .

# Run locally
run: build
	./$(BINARY_NAME) -config config.hujson

# Install deps
deps:
	go mod download
	go mod tidy

# Development cycle
dev: clean lint test build

# Docker run
docker-run: docker
	docker run -p 53:53/udp -v ./config.hujson:/config.hujson $(BINARY_NAME)

# Release: test, build, lint, and push multi-arch images to GHCR
release: test build lint docker-multiarch

# CI/CD targets for GitHub Actions
ci-test: test lint build
ci-docker: docker-multiarch

# Kubernetes deployment targets
k8s-secret:
	@if [ -z "$(AUTHKEY)" ]; then \
		echo "Error: AUTHKEY is required. Usage: make k8s-deploy AUTHKEY=tskey-auth-..."; \
		exit 1; \
	fi
	kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	kubectl create secret generic tailscale-auth \
		--from-literal=authkey=$(AUTHKEY) \
		--namespace=$(NAMESPACE) \
		--dry-run=client -o yaml | kubectl apply -f -

k8s-deploy: k8s-secret
	@if [ -z "$(AUTHKEY)" ]; then \
		echo "Error: AUTHKEY is required. Usage: make k8s-deploy AUTHKEY=tskey-auth-..."; \
		exit 1; \
	fi
	cd deploy/k8s/base && kustomize edit set image tsdnsreflector=$(REGISTRY)/$(IMAGE_NAME):$(TAG)
	kubectl apply -k deploy/k8s/base
	@echo "Deployment complete. Check status with: make k8s-logs"

k8s-undeploy:
	kubectl delete -k deploy/k8s/base --ignore-not-found=true
	kubectl delete secret tailscale-auth --namespace=$(NAMESPACE) --ignore-not-found=true

k8s-logs:
	kubectl logs -f statefulset/tsdnsreflector -n $(NAMESPACE)

k8s-status:
	kubectl get pods,svc,secrets,statefulsets -n $(NAMESPACE)
	kubectl describe statefulset tsdnsreflector -n $(NAMESPACE)