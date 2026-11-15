# ADR-002: Acesso a Dados com `sqlc` + `pgx` em vez de ORM

- **Status:** Aceito
- **Data:** 2026-11-15

## Contexto

O domínio da doceria envolve operações transacionais críticas: reserva de estoque com concorrência, baixa de insumos a partir de fichas técnicas, geração de lotes de produção, snapshots de custos/preços e queries analíticas para relatórios de margem e vendas.

## Decisão

Utilizar **`pgx/v5`** (driver nativo PostgreSQL de alta performance) combinado com **`sqlc`** (gerador de código Go type-safe a partir de arquivos `.sql`) e **`goose`** para controle de migrations, evitando ORMs como GORM ou Ent.

## Consequências

- **Positivas:**
  - Controle total sobre o SQL executado, índices, `SELECT ... FOR UPDATE`, CTEs e queries agregadas de relatórios sem abstrações opacas ou problema N+1 acidental.
  - Verificação em tempo de compilação dos tipos entre SQL e Go gerada pelo `sqlc`.
  - Facilidade de gerenciar transações explícitas (`pgx.Tx`) na camada `repository`/`service`.
- **Negativas / Trade-offs:**
  - Queries com muitos filtros dinâmicos (como listagem de pedidos no admin) exigem cuidado na escrita de SQL condicional (ex.: `sqlc.narg(...)` com `WHERE (@status::text IS NULL OR status = @status)`).
  - Exige conhecimento sólido de SQL por parte de quem mantém o código.
