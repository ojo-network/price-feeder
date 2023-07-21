# Builder
FROM golang:1.19-alpine AS builder

RUN apk add --no-cache \
    ca-certificates \
    build-base \
    linux-headers \
    git

WORKDIR /app

COPY . .
RUN mv ./umee ../umee
# RUN go mod download

# Cosmwasm - Download correct libwasmvm version
RUN wget https://github.com/CosmWasm/wasmvm/releases/download/v1.2.4/libwasmvm_muslc.x86_64.a \
      -O /lib/libwasmvm_muslc.a
    # verify checksum
RUN wget https://github.com/CosmWasm/wasmvm/releases/download/v1.2.4/checksums.txt -O /tmp/checksums.txt && \
    sha256sum /lib/libwasmvm_muslc.a | grep $(cat /tmp/checksums.txt | grep x86_64.a | cut -d ' ' -f 1)

RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build

# Runner
FROM alpine
RUN apk add bash

WORKDIR /app

COPY --from=builder /app/build/price-feeder /bin/price-feeder
COPY --from=builder /app/price-feeder.example.toml /app/price-feeder.toml

EXPOSE 7171

ENTRYPOINT ["price-feeder", "price-feeder.toml"]
