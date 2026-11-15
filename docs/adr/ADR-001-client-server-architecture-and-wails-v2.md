# ADR-001: Arquitetura Cliente-Servidor com API Central e Wails v2 (Ponte SSE)

- **Status:** Aceito
- **Data:** 2026-11-15

## Contexto

O sistema precisa atender dois clientes distintos:
1. Um aplicativo desktop interno usado pelos funcionários da doceria (balcão, cozinha, entregas, gestão de estoque e relatórios), com necessidade de impressão de comanda (térmica 80mm / PDF) e notificações em tempo real.
2. Uma loja online pública com foco em SEO e carregamento rápido em dispositivos móveis.

Além disso, o projeto tem como premissa que a API seja a única fonte de verdade e nenhum frontend acesse o PostgreSQL diretamente.

## Decisão

1. **API Central Única em Go (`chi`)**: centraliza todas as regras de negócio, autenticação/RBAC, transações de estoque e integração de pagamentos, expondo rotas `/api/v1/public/*` e `/api/v1/admin/*`.
2. **Wails v2 para o Desktop (com Vue 3 + TypeScript)**: optou-se pelo Wails **v2** (linha estável de produção) em vez do Wails v3 (ainda em estágio alpha) ou Electron (maior consumo de memória e binário pesado).
3. **Camada fina + Ponte SSE no Go do Wails**:
   - Os tokens JWT (access e refresh) são armazenados no keyring do sistema operacional pelo processo Go do Wails, sem ficarem expostos no `localStorage` da WebView.
   - Como a API `EventSource` do navegador/WebView não permite enviar cabeçalhos `Authorization: Bearer`, o processo Go do Wails abre e mantém a conexão SSE em `/api/v1/admin/orders/stream` (com reconexão automática exponencial) e repassa os eventos para a interface Vue via `runtime.EventsEmit`.
4. **SSE em vez de WebSocket na API**: a comunicação em tempo real é unidirecional (servidor → desktop para `order.created`, `order.updated`, `order.expired` e `stock.low`). SSE funciona sobre HTTP padrão, simplifica proxies reversos e reconexão.

## Consequências

- **Positivas:** regras de negócio não são duplicadas; maior segurança para credenciais no desktop; stack uniforme em Go entre API e camada nativa do desktop; binários leves para Windows/macOS/Linux.
- **Negativas / Trade-offs:** o app desktop depende de conexão com a internet na v1 (sem banco local/offline); se no futuro houver necessidade de comunicação bidirecional em tempo real (ex.: rastreio contínuo de entregador via GPS), será necessário evoluir para WebSocket apenas nesse canal.
