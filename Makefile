.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt
GOLANGCI_LINT ?= .tools/golangci-lint
GOLANGCI_LINT_VERSION := v2.12.2
TOOLS_DIR := $(CURDIR)/.tools
COVERAGE_DIR := coverage
COMPOSE ?= docker compose
COMPOSE_FILE ?= docker-compose.yml
FIXTURE_PROJECT ?= minio-s3-api-tests
FIXTURE_COMPOSE_FILE ?= docker-compose.test.yml
BASE_URL ?= http://127.0.0.1:8081

.PHONY: help run build fmt fmt-check vet test test-race test-cover test-integration fixtures-up fixtures-down lint tools deps docs docs-check check clean health docker-config docker-build docker-up docker-down docker-logs docker-ps

help: ## Показать команды API
	@awk 'BEGIN {FS = ":.*##"; print "make <команда>\n"} /^[a-zA-Z_-]+:.*##/ {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

run: ## Запустить API локально
	$(GO) run ./src

build: ## Собрать bin/api
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o bin/api ./src

fmt: ## Отформатировать Go-код
	$(GO) fmt ./...

fmt-check: ## Проверить форматирование без изменения файлов
	@files="$$(find src tests tools -type f -name '*.go' -exec $(GOFMT) -l {} +)" || exit $$?; \
	test -z "$$files" || { printf '%s\n\nЗапустите make fmt\n' "$$files"; exit 1; }

vet: ## Проверить код через go vet
	$(GO) vet ./...

test: ## Запустить тесты
	$(GO) test ./...

test-race: ## Запустить тесты с race detector
	$(GO) test -race ./...

test-integration: ## Запустить интеграционные тесты с уже работающими fixtures
	MINIO_INTEGRATION=1 $(GO) test -race -count=1 ./...

fixtures-up: ## Собрать и запустить изолированные MinIO fixtures (tiers и pools)
	$(COMPOSE) -p $(FIXTURE_PROJECT) -f $(FIXTURE_COMPOSE_FILE) --profile tiers --profile pools up -d --build --wait

fixtures-down: ## Остановить изолированные MinIO fixtures и удалить их tmpfs-данные
	$(COMPOSE) -p $(FIXTURE_PROJECT) -f $(FIXTURE_COMPOSE_FILE) --profile tiers --profile pools down

test-cover: ## Запустить тесты и сохранить coverage/coverage.out
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	$(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out

lint: ## Запустить golangci-lint (установка: make tools)
	@command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1 || { printf '%s\n' 'Не найден golangci-lint. Запустите make tools.'; exit 1; }
	$(GOLANGCI_LINT) run --config .golangci.yml ./...

tools: ## Установить golangci-lint фиксированной версии в .tools
	@mkdir -p "$(TOOLS_DIR)"
	GOBIN="$(TOOLS_DIR)" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

deps: ## Скачать и проверить зависимости без изменения go.mod
	$(GO) mod download
	$(GO) mod verify

docs: ## Обновить OpenAPI по контрактам маршрутов
	$(GO) run ./tools/openapi

docs-check: ## Проверить актуальность OpenAPI
	$(GO) run ./tools/openapi -check

check: fmt-check docs-check lint test-race build ## Проверить форматирование, OpenAPI, lint, race и сборку

clean: ## Удалить только локальные результаты сборки и coverage
	rm -rf bin coverage

health: ## Проверить работающий API
	curl --fail --silent --show-error "$(BASE_URL)/health"

docker-config: ## Проверить Compose без вывода значений env
	$(COMPOSE) -f $(COMPOSE_FILE) config --quiet

docker-build: ## Собрать production-образ API
	$(COMPOSE) -f $(COMPOSE_FILE) build

docker-up: ## Собрать и запустить API в Docker, дождаться healthcheck
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --build --wait

docker-down: ## Остановить standalone API
	$(COMPOSE) -f $(COMPOSE_FILE) down

docker-logs: ## Показать логи API
	$(COMPOSE) -f $(COMPOSE_FILE) logs --tail=100 -f

docker-ps: ## Показать состояние API-контейнера
	$(COMPOSE) -f $(COMPOSE_FILE) ps
