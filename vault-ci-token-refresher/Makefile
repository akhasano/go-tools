all:
	CGO_ENABLED=0 GOOS=linux go build -o ./vault-token-refresher .
	bash -c "source .env"
	docker build -t registry.test.int/vault-token-refresher .
	docker push registry.test.int/vault-token-refresher
