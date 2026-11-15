# ADR-003: Chaves Primárias `UUIDv7` + `order_number` Sequencial em Pedidos

- **Status:** Aceito
- **Data:** 2026-11-15

## Contexto

O sistema expõe URLs públicas para acompanhamento de pedidos e recursos de catálogo, além de ter tabelas de alto crescimento como `orders`, `order_items` e `stock_movements`. Precisamos escolher entre `BIGSERIAL`, `UUIDv4` ou `UUIDv7` como estratégia de chave primária.

## Decisão

1. Adotar **`UUIDv7`** como chave primária (`UUID`) em todas as tabelas do banco, gerado na aplicação Go (`uuid.NewV7()`).
2. Adicionar uma coluna **`order_number` (`BIGSERIAL` / sequência amigável)** exclusiva na tabela `orders` para uso humano (painel da cozinha, atendentes, cliente e impressão de comanda, ex.: `#1042`).
3. Para acompanhamento público de pedido na web (`GET /api/v1/public/orders/{id}?token=...`), utilizar um `public_token` criptograficamente aleatório (separado do ID e do `order_number`).

## Consequências

- **Positivas:**
  - `UUIDv7` é ordenado cronologicamente pelo timestamp embutido nos bits mais significativos, evitando fragmentação de índices B-Tree no PostgreSQL (ao contrário do `UUIDv4`).
  - Não expõe volume de vendas ou sequência previsível nas rotas de API via ID, enquanto `order_number` mantém a ergonomia operacional na loja.
  - IDs podem ser gerados antes da persistência em Go (útil para idempotência e testes).
- **Negativas / Trade-offs:**
  - Ocupa 16 bytes por chave/FK contra 8 bytes de um `BIGINT`, impacto irrelevante para a escala single-tenant de uma doceria.
