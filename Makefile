.PHONY: help up down logs ps tools setup-hooks lint fmt test generate migrate-up migrate-down migrate-status dev-api

# Variáveis de ambiente padrão (sobrescritas pelo .env se existir)
ifneq (,$(wildcard .env))
	include .env
	export
endif

DATABASE_URL ?= postgres://doceria:doceria_dev_password@localhost:5432/doceria?sslmode=disable
GOOSE_DIR := api/db/migrations

# Versões fixadas compatíveis com Go 1.27+
GOLANGCI_LINT_VERSION := v2.15.0
SQLC_VERSION := v1.31.1
GOOSE_VERSION := v3.30.1
OAPI_CODEGEN_VERSION := v2.8.2

help: ## Mostra os comandos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

up: ## Sobe os containers de infraestrutura (Postgres)
	docker compose up -d

down: ## Para e remove os containers
	docker compose down

logs: ## Exibe logs dos containers
	docker compose logs -f

ps: ## Lista status dos containers
	docker compose ps

tools: ## Instala ferramentas CLI de desenvolvimento (golangci-lint, sqlc, goose, oapi-codegen)
	@echo "Instalando golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION)
	@echo "Instalando sqlc $(SQLC_VERSION)..."
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	@echo "Instalando goose $(GOOSE_VERSION)..."
	go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	@echo "Instalando oapi-codegen $(OAPI_CODEGEN_VERSION)..."
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)

setup-hooks: ## Configura os hooks Git locais (.githooks)
	git config core.hooksPath .githooks
	chmod +x .githooks/*
	@echo "Git hooks configurados em .githooks/"

lint: ## Executa lint nos módulos existentes
	@if [ -f api/go.mod ]; then \
		cd api && golangci-lint run --config ../.golangci.yml ./...; \
	else \
		echo "api/go.mod ainda não existe (será criado na Fase 1)."; \
	fi

fmt: ## Formata o código Go
	@if [ -d api ]; then \
		gofmt -w -s api/; \
	fi

test: ## Executa os testes nos módulos existentes
	@if [ -f api/go.mod ]; then \
		cd api && go test -race -v ./...; \
	else \
		echo "api/go.mod ainda não existe (será criado na Fase 1)."; \
	fi

generate: ## Gera código a partir do OpenAPI e sqlc
	@if [ -f api/sqlc.yaml ]; then \
		cd api && sqlc generate; \
	fi
	@if [ -f api/go.mod ]; then \
		cd api && go generate ./...; \
	fi

migrate-up: ## Aplica todas as migrations pendentes
	goose -dir $(GOOSE_DIR) postgres "$(DATABASE_URL)" up

migrate-down: ## Reverte a última migration
	goose -dir $(GOOSE_DIR) postgres "$(DATABASE_URL)" down

migrate-status: ## Exibe o status das migrations
	goose -dir $(GOOSE_DIR) postgres "$(DATABASE_URL)" status

dev-api: ## Inicia a API localmente
	cd api && go run ./cmd/server
