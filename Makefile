# =============================================================================
# tsure ERP - Makefile do monorepo
#
# Requisitos no PATH:
#   - go 1.22+               (apps/web)
#   - gradle                 (apps/mobile)
#   - psql                   (banco)
#   - sh-compatible shell    (Git Bash, WSL, Linux, macOS)
#
# Carrega .env da raiz se existir (sem falhar quando ausente).
# =============================================================================

-include .env
export

# ---- Defaults sobrescritiveis por .env ou linha de comando ------------------
DATABASE_URL ?= postgres://tsure:tsure@127.0.0.1:5432/tsure?sslmode=disable
ADDR         ?= 127.0.0.1:3456
TSURE_ENV    ?= dev

# Ajustes por SO --------------------------------------------------------------
ifeq ($(OS),Windows_NT)
  BIN_EXT := .exe
  RM_RF   := powershell -NoProfile -Command "Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
else
  BIN_EXT :=
  RM_RF   := rm -rf
endif

.DEFAULT_GOAL := help

# =============================================================================
# Ajuda
# =============================================================================
.PHONY: help
help: ## Lista os comandos disponiveis
	@awk 'BEGIN {FS = ":.*?## "; printf "\nAlvos:\n"} \
	      /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' \
	      $(MAKEFILE_LIST)

# =============================================================================
# Dev rapido
# =============================================================================
.PHONY: dev
dev: db-init web ## Aplica schema+seed e sobe o web

# =============================================================================
# Web (Go SSR + HTMX)
# =============================================================================
.PHONY: web web-build web-run web-test web-vet web-tidy
web: web-run ## Alias para web-run

web-run: ## Roda o web em dev (go run)
	cd apps/web && go run .

web-build: ## Compila o binario em bin/tsure-web
	@mkdir -p bin
	cd apps/web && go build -o ../../bin/tsure-web$(BIN_EXT) .

web-test: ## Roda os testes do web
	go test ./apps/web/...

web-vet: ## go vet em apps/web
	go vet ./apps/web/...

web-tidy: ## go mod tidy
	go mod tidy

# =============================================================================
# Mobile (Android)
# =============================================================================
.PHONY: mobile mobile-build mobile-install mobile-clean
mobile: mobile-install ## Alias para mobile-install

mobile-build: ## Compila APK debug
	cd apps/mobile && gradle assembleDebug

mobile-install: ## Instala APK debug no dispositivo conectado
	cd apps/mobile && gradle installDebug

mobile-clean: ## Limpa artefatos do mobile
	cd apps/mobile && gradle clean

# =============================================================================
# Banco de dados
# =============================================================================
.PHONY: db-init db-schema db-seed db-reset db-psql
db-init: db-schema db-seed ## Aplica schema.sql + seed.sql

db-schema: ## Aplica database/schema.sql
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f database/schema.sql

db-seed: ## Aplica database/seed.sql
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f database/seed.sql

db-reset: ## DROP schema public e recria (apaga TUDO)
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 \
	    -c "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"
	$(MAKE) db-init

db-psql: ## Abre um shell psql na database
	psql "$(DATABASE_URL)"

# =============================================================================
# Higiene
# =============================================================================
.PHONY: tidy clean
tidy: web-tidy ## Roda go mod tidy

clean: ## Remove binarios e caches locais
	$(RM_RF) bin
	cd apps/web && go clean
