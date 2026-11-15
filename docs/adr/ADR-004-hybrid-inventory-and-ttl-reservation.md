# ADR-004: Modelo Híbrido de Estoque, Lotes de Produção e Reserva com TTL

- **Status:** Aceito
- **Data:** 2026-11-15

## Contexto

Em uma doceria real, existem dois fluxos de produção distintos:
1. **Pronta-entrega / vitrine** (ex.: brigadeiros, fatias de torta, bolos de pote): produzidos em fornadas/lotes que consomem insumos antes da venda e geram estoque do produto pronto por variante.
2. **Sob encomenda** (ex.: bolos decorados agendados): produzidos apenas após o pedido, consumindo os insumos da ficha técnica diretamente.

Além disso, no checkout online via Pix, há um intervalo entre a criação do pedido e a confirmação do pagamento pelo webhook. Sem reserva, dois clientes podem comprar o último item simultaneamente; sem expiração, um cliente que abandona o Pix trava o estoque indefinidamente.

## Decisão

1. **Estoque Híbrido por Tipo de Produto (`pronta_entrega` vs. `sob_encomenda`)**:
   - Variantes (`ProductVariant`) de pronta-entrega mantêm `estoque_atual` e `estoque_reservado`, abastecidos via registros de `ProductionBatch` (que baixam `Materials` conforme a `Recipe` em transação única).
   - Variantes sob encomenda reservam/consomem diretamente os `Materials` da `Recipe`.
2. **Reserva na Criação + Baixa na Confirmação + Worker TTL**:
   - Ao criar um pedido (`novo`), o saldo necessário é incrementado em `estoque_reservado` (validando `estoque_atual - estoque_reservado >= qtd` com lock pessimista `FOR UPDATE`).
   - Pedidos com `forma_pagamento = pix_online` recebem `reserva_expira_em = now() + 15m`. Um worker periódico na API cancela pedidos expirados, libera a reserva e notifica via SSE (`order.expired`).
   - Na confirmação do pedido (webhook Pix ou aceite manual em pedidos com pagamento na entrega/retirada), a reserva é convertida em baixa definitiva gerando `StockMovement`.
3. **Snapshot de Custo no `OrderItem`**:
   - Cada item congela `preco_snapshot` e `custo_unitario_snapshot` (calculado da receita com o `custo_unitario_medio` vigente no momento do pedido) para que o relatório de margem bruta histórica seja fiel mesmo após reajustes de insumos.

## Consequências

- **Positivas:** reflete fielmente a operação real de uma doceria; evita overselling em picos de acesso; preserva integridade financeira histórica.
- **Negativas / Trade-offs:** aumenta a complexidade transacional na criação/cancelamento de pedidos e exige testes de concorrência rigorosos na Fase 5.
