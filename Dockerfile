# Builder
FROM golang:1.20-alpine AS builder

RUN apk add --no-cache \
    ca-certificates \
    build-base \
    linux-headers \
    git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# Cosmwasm - Download correct libwasmvm version
RUN WASMVM_VERSION=$(go list -m github.com/CosmWasm/wasmvm | cut -d ' ' -f 2) && \
    wget https://github.com/CosmWasm/wasmvm/releases/download/$WASMVM_VERSION/libwasmvm_muslc.$(uname -m).a \
      -O /lib/libwasmvm_muslc.a && \
    # verify checksum
    wget https://github.com/CosmWasm/wasmvm/releases/download/$WASMVM_VERSION/checksums.txt -O /tmp/checksums.txt && \
    sha256sum /lib/libwasmvm_muslc.a | grep $(cat /tmp/checksums.txt | grep $(uname -m).a | cut -d ' ' -f 1)

COPY . .
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build

# Runner
FROM alpine
RUN apk add bash

WORKDIR /app

COPY --from=builder /app/build/price-feeder /bin/price-feeder
COPY --from=builder /app/price-feeder.example.toml /app/price-feeder.toml
COPY --from=builder /app/umee-provider-config/ /app/umee-provider-config/

EXPOSE 7171

ENTRYPOINT ["price-feeder", "price-feeder.toml"]
