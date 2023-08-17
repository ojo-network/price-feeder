BRANCH    := $(shell git rev-parse --abbrev-ref HEAD)
BUILD_DIR ?= $(CURDIR)/build
COMMIT    := $(shell git log -1 --format='%H')
SDK_VERSION     := $(shell go list -m github.com/cosmos/cosmos-sdk | sed 's:.* ::')

all: test-unit install

.PHONY: all

###############################################################################
##                                  Version                                  ##
###############################################################################

ifeq (,$(VERSION))
  VERSION := $(shell git describe --tags --always | sed 's:.* ::')
  # if VERSION is empty, then populate it with branch's name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

###############################################################################
##                              Build / Install                              ##
###############################################################################

ldflags = -X github.com/ojo-network/price-feeder/cmd.Version=$(VERSION) \
		  -X github.com/ojo-network/price-feeder/cmd.Commit=$(COMMIT) \
		  -X github.com/ojo-network/price-feeder/cmd.SDKVersion=$(SDK_VERSION)

ifeq ($(LINK_STATICALLY),true)
	ldflags += -linkmode=external -extldflags "-Wl,-z,muldefs -static"
endif

build_tags += $(BUILD_TAGS)

BUILD_FLAGS := -tags "$(build_tags)" -ldflags '$(ldflags)'

build: go.sum
	@echo "--> Building..."
	go build -mod=readonly -o $(BUILD_DIR)/ $(BUILD_FLAGS) ./...

install: go.sum
	@echo "--> Installing..."
	go install -mod=readonly $(BUILD_FLAGS) ./...

.PHONY: build install

###############################################################################
##                                  Docker                                   ##
###############################################################################

docker-build:
	@DOCKER_BUILDKIT=1 docker build -t ghcr.io/ojo-network/price-feeder-ojo .

docker-push:
	@docker push ghcr.io/ojo-network/price-feeder-ojo

.PHONY: docker-build docker-push

###############################################################################
##                              Tests & Linting                              ##
###############################################################################

test-unit:
	@echo "--> Running tests"
	@go test -short -mod=readonly -race ./... -v

.PHONY: test-unit

test-integration:
	@echo "--> Running Integration Tests"
	@go test -mod=readonly ./tests/integration/... -v

.PHONY: test-integration

lint:
	@echo "--> Running linter"
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint run --fix --timeout=8m

.PHONY: lint
