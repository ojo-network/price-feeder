# Builder
FROM golang:1.19-alpine AS builder

RUN apk add --no-cache \
    ca-certificates \
    build-base \
    linux-headers \
    git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make build

# Runner
FROM alpine
RUN apk add bash

WORKDIR /app

COPY --from=builder /app/build/price-feeder /bin/price-feeder
COPY --from=builder /app/price-feeder.example.toml /app/price-feeder.toml

EXPOSE 7171

ENTRYPOINT ["price-feeder", "price-feeder.toml"]
