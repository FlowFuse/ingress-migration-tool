.PHONY: help build clean lint fmt vet check-quality
# APP_NAME=flowfuse-device-installer
# VERSION:=development
BUILDDIR=./build
APP_NAME=ingress-migration-tool

# Optional colors
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
WHITE  := $(shell tput -Txterm setaf 7)
CYAN   := $(shell tput -Txterm setaf 6)
RESET  := $(shell tput -Txterm sgr0)

default: help

build: ## builds the application for all platforms
	mkdir -p build
	go build -o ${BUILDDIR}/${APP_NAME} ./

build-docker: ## builds the Docker image
	docker build -t ${APP_NAME}:latest .

clean: ## cleans the build artifacts
	go clean
	rm -rf ${BUILDDIR}/*

## Quality checks
check-quality: ## runs code quality checks
	make lint
	make fmt
	make vet

lint: ## go linting. Update and use specific lint tool and options
	golangci-lint run .

vet: ## run static analysis
	go vet ./...

fmt: ## runs go formatter
	go fmt ./...

## Help
help: ## Show this help.
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} { \
		if (/^[a-zA-Z_-]+:.*?##.*$$/) {printf "    ${YELLOW}%-20s${GREEN}%s${RESET}\n", $$1, $$2} \
		else if (/^## .*$$/) {printf "  ${CYAN}%s${RESET}\n", substr($$1,4)} \
		}' $(MAKEFILE_LIST)
