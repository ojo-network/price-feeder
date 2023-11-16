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
RUN go build -o build/monitor cmd/monitor/main.go

# Runner
FROM alpine
RUN apk add bash

WORKDIR /app

COPY --from=builder /app/build/monitor /bin/monitor
COPY --from=builder /app/price-feeder.example.toml /app/price-feeder.example.toml
COPY --from=builder /app/ojo-provider-config/ /app/ojo-provider-config/

EXPOSE 7171

ENTRYPOINT ["monitor"]
