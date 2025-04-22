.PHONY: build test run docker-build k8s-deploy k8s-deploy-alt clean

# Variables
IMAGE_NAME := tsdnsreflector
BINARY_NAME := tsdnsreflector

build:
	go build -o $(BINARY_NAME) .

test:
	go test -v ./...

run: build
	./$(BINARY_NAME) --reflected-domain=cluster1.local --original-domain=cluster.local --siteid=1 --force-4via6=true

run-alt: build
	./$(BINARY_NAME) --reflected-domain=cluster1.local --original-domain=cluster.local --siteid=1 --dns-resolver=fdbb:cbf8:2702::a --force-4via6=false

docker-build:
	docker build -t $(IMAGE_NAME):latest .

docker-run: docker-build
	docker run -p 53:53/udp -p 53:53/tcp \
		-e SITEID=1 \
		-e REFLECTED_DOMAIN=cluster1.local \
		-e ORIGINAL_DOMAIN=cluster.local \
		-e FORCE_4VIA6=true \
		$(IMAGE_NAME):latest

docker-run-alt: docker-build
	docker run -p 53:53/udp -p 53:53/tcp \
		-e SITEID=1 \
		-e REFLECTED_DOMAIN=cluster1.local \
		-e ORIGINAL_DOMAIN=cluster.local \
		-e DNS_RESOLVER=fdbb:cbf8:2702::a \
		-e FORCE_4VIA6=false \
		$(IMAGE_NAME):latest

k8s-deploy:
	kubectl apply -k kubernetes/

k8s-deploy-alt:
	kubectl apply -f kubernetes/configmap-alt.yaml
	kubectl apply -f kubernetes/deployment.yaml
	kubectl apply -f kubernetes/service.yaml

clean:
	rm -f $(BINARY_NAME)
	go clean 