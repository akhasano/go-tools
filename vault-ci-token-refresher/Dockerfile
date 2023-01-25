# FROM golang:1.18.10-alpine3.17 as builder
# WORKDIR /build
# COPY go.mod .
# RUN go mod download
# COPY . .
# RUN go build -o /vault-token-refresher vault-token-refresher.go

# FROM golang:1.18.10-alpine3.17
# COPY --from=builder vault-token-refresher /bin/vault-token-refresher
# ENTRYPOINT ["/bin/vault-token-refresher"]

FROM alpine:3.17.0
COPY ./vault-token-refresher /vault-token-refresher
ENTRYPOINT /vault-token-refresher
