# Doceria — Sistema de Gestão e Loja Online

Sistema completo para gestão de uma doceria (single-tenant), composto por:

1. **API Central (`api/`)** em Go + PostgreSQL (`chi`, `sqlc`, `pgx`, `goose`, SSE, OpenAPI Spec-First).
2. **App Desktop (`desktop/`)** em Wails v2 + Vue 3 + TypeScript para operação interna (pedidos em tempo real, cozinha/KDS, estoque híbrido, fichas técnicas, lotes de produção, entregas e relatórios).
3. **Loja Online / Landing (`web/`)** em Nuxt 3 + TypeScript com SSR/SSG, cardápio público, checkout com Pix online ou pagamento na entrega/retirada e acompanhamento de pedido.

> Para o plano completo de arquitetura e fases de implementação, consulte [ARQUITETURA.md](./ARQUITETURA.md) e os registros de decisão em [docs/adr/](./docs/adr/README.md).

## Pré-requisitos

- **Go** `1.23+`
- **Docker** e **Docker Compose**
- **Node.js** `20+` (para as fases de frontend)
- **Make**

## Setup inicial (Fase 0)

1. Copie as variáveis de ambiente:
   ```bash
   cp .env.example .env
   ```

2. Configure os hooks Git locais (Conventional Commits e validação de formatação/.env):
   ```bash
   make setup-hooks
   ```

3. Instale as ferramentas CLI de desenvolvimento (`golangci-lint`, `sqlc`, `goose`, `oapi-codegen`):
   ```bash
   make tools
   ```

4. Suba o banco PostgreSQL:
   ```bash
   make up
   ```

## Comandos úteis

Execute `make help` para listar todos os alvos disponíveis no `Makefile`.
