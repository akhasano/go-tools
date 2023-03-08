all:
	rm -f k8s-events-webhook
	CGO_ENABLED=0 GOOS=linux go build k8s-events-webhook.go
	docker build -t registry.domain.com/devops:k8s-events-webhook-dev  .
	docker push registry.domain.com/devops:k8s-events-webhook-dev
	rm -f k8s-events-webhook