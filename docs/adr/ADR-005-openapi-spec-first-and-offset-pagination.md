# ADR-005: Contrato OpenAPI Spec-First e Paginação `limit/offset`

- **Status:** Aceito
- **Data:** 2026-11-15

## Contexto

O projeto possui três artefatos que precisam evoluir em sincronia: API em Go, app Desktop (Wails/Go + Vue/TS) e loja online (Nuxt/TS). Além disso, endpoints administrativos possuem listagens com múltiplos filtros dinâmicos e navegação por páginas em tabelas.

## Decisão

1. **Abordagem OpenAPI Spec-First (`docs/api/openapi.yaml`)**:
   - O arquivo `docs/api/openapi.yaml` é evoluído em cada fase antes ou junto com a implementação dos handlers.
   - Os DTOs e interfaces de servidor Go são gerados via `oapi-codegen`, e os tipos/clientes TypeScript são gerados via `openapi-typescript`.
2. **Paginação `limit/offset` padronizada**:
   - Endpoints paginados aceitam parâmetros `page` (default `1`) e `page_size` (default `20`, máximo `100`) e retornam envelope com `data` e `meta` (`page`, `page_size`, `total_items`, `total_pages`).

## Consequências

- **Positivas:**
  - Contrato único e auditável desde a Fase 0, evitando divergências entre backend e frontends.
  - Paginação `limit/offset` oferece experiência direta em tabelas administrativas (ir para página N, exibir total de registros) com excelente desempenho para o volume single-tenant quando combinada a índices compostos.
- **Negativas / Trade-offs:**
  - Qualquer alteração de rota/campo exige atualizar o `openapi.yaml` e rodar `make generate`.
  - Em tabelas com milhões de linhas, `OFFSET` profundo degrada; caso alguma tabela atinja esse porte futuramente, pode-se migrar apenas o endpoint específico para keyset/cursor pagination.
