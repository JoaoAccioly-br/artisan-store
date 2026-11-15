# Arquitetura do Sistema

Este documento resume a arquitetura de alto nível do sistema. A especificação detalhada do domínio e do roadmap encontra-se em [ARQUITETURA.md](../ARQUITETURA.md).

## Visão de Contêineres

```mermaid
flowchart TB
    Customer["Cliente (Navegador)"] -->|HTTPS| Web["Loja Online (Nuxt 3)"]
    Staff["Funcionário (Loja/Cozinha)"] --> Desktop["App Desktop (Wails v2 + Vue 3)"]

    Web -->|HTTPS REST| API["API Central (Go + chi)"]
    Desktop -->|HTTPS REST + JWT| API
    API -->|SSE /orders/stream| Desktop

    API -->|pgx + sqlc| DB[("PostgreSQL 16")]
    API -->|Webhook / Cobrança| Pix["Gateway Pix"]
    API -->|Upload / Leitura| Storage["Storage de Imagens (Local / S3)"]
```

## Decisões Arquiteturais

Consulte o índice de ADRs em [docs/adr/README.md](./adr/README.md).
