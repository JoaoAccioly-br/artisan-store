# Doceria — Arquitetura e System Design

> Documento de contexto para ser entregue a uma IA de desenvolvimento.
> Leia tudo antes de gerar qualquer código. Siga as fases na ordem e **pare ao final de cada fase** para eu revisar.

---

## 1. Visão geral do projeto

Sistema completo para uma **loja de doces**, composto por:

1. **App desktop** (uso interno dos funcionários): gerenciamento de pedidos, materiais/estoque, lotes de produção, receitas, entregas, clientes e relatórios.
2. **Landing page / loja online** (uso público): clientes veem o cardápio e fazem pedidos pela web.
3. **API em Go** (backend único e compartilhado) que serve os dois frontends.

**Objetivo secundário:** o projeto é para **portfólio**. Portanto, priorize código limpo, testes, documentação, CI/CD, decisões arquiteturais justificadas (ADRs) e um README forte com screenshots e diagrama.

### Premissas

- Uma única loja (single-tenant). Multi-tenant está fora do escopo.
- Idioma da interface: **português (pt-BR)**. Moeda: BRL. Fuso: America/Sao_Paulo.
- O desktop depende de internet (sem modo offline na v1).
- Pagamento no checkout web e balcão via **Pix online** (com QR Code/copia-e-cola e webhook, mockável em desenvolvimento) ou **Pagamento na entrega/retirada** (cartão na maquininha ou dinheiro com cálculo de troco).

---

## 2. Stack tecnológica

| Camada | Tecnologia |
|---|---|
| Desktop | **Wails v2** (linha estável de produção) + Vue 3 + TypeScript + Pinia + Vue Router |
| Landing/Loja | **Nuxt 3** (SSR/SSG para SEO) + TypeScript + Pinia |
| UI | Tailwind CSS (ambos os frontends); componentes compartilhados via pacote interno se fizer sentido |
| API | **Go** (versão estável mais recente) + **chi** (router) |
| Banco | **PostgreSQL** |
| Acesso a dados | **sqlc** + **pgx**; migrations com **goose** |
| Auth | JWT (access + refresh) para funcionários; pedidos públicos sem login (identificação por telefone/e-mail) |
| Tempo real | **SSE** (Server-Sent Events) para notificar o desktop de novos pedidos; no desktop, o processo Go do Wails consome o stream SSE e repassa via `runtime.EventsEmit` para o Vue |
| Validação | `go-playground/validator` (Go) e Zod (frontends) |
| Config | variáveis de ambiente (`envconfig` ou `viper`) |
| Logs | `log/slog` estruturado |
| Testes | `testing` + `testify` + `testcontainers-go` (Go); Vitest (Vue); Playwright (E2E da landing) |
| Docs da API | **OpenAPI 3 Spec-First** (`docs/api/openapi.yaml` evoluído a cada fase, gerando código com `oapi-codegen` no Go e `openapi-typescript` nos frontends) |
| Containers | Docker + docker-compose |
| CI/CD | GitHub Actions |
| Hospedagem | API + Postgres em VPS ou Fly.io/Railway; landing em Vercel/Netlify/Cloudflare Pages |

---

## 3. Arquitetura de alto nível

```
┌─────────────────┐        ┌──────────────────┐
│  App Desktop    │        │  Landing / Loja  │
│  Wails v2 + Vue │        │  Nuxt 3          │
│  (funcionários) │        │  (clientes)      │
└────────┬────────┘        └────────┬─────────┘
         │ HTTPS + JWT              │ HTTPS (rotas públicas)
         │ SSE (Go Wails → Events)  │
         └────────────┬─────────────┘
                      ▼
           ┌─────────────────────┐
           │      API em Go      │
           │  handlers → services│
           │  → repositories     │
           └───────┬─────────────┘
                   │
        ┌──────────┼───────────┐
        ▼          ▼           ▼
   PostgreSQL   Gateway Pix   Storage de imagens
                (webhook)     (S3 compatível / disco)
```

### Princípios

- **API é a única fonte de verdade.** Nenhum frontend acessa o banco diretamente.
- **Contrato OpenAPI Spec-First:** `docs/api/openapi.yaml` é atualizado antes/durante cada fase da API e usado para gerar DTOs/interfaces e clientes HTTP.
- **Separação de rotas:** `/api/v1/public/*` (sem auth, com rate limit) e `/api/v1/admin/*` (JWT + papel).
- **Camadas na API:** `handler` (HTTP) → `service` (regra de negócio) → `repository` (SQL via sqlc). Handlers não contêm regra de negócio.
- **Injeção de dependências manual** via construtores (sem framework de DI).
- **Erros de domínio tipados**, mapeados para códigos HTTP em um único lugar.
- **Dinheiro em centavos (`int64`)**, nunca float.
- **Quantidades de insumos e estoque padronizadas** em unidades base inteiras (`g`, `ml`, `un` ou escala fixa milésima `int64`) para evitar erros de ponto flutuante.
- **Datas em UTC no banco**, convertidas para America/Sao_Paulo na apresentação.
- **Idempotência** na criação de pedidos públicos (header `Idempotency-Key`) para evitar duplicidade.

---

## 4. Modelo de domínio

### Entidades principais

- **User** (funcionário): `id` (UUIDv7), `nome`, `email`, `senha_hash` (argon2id), `papel` (`admin`, `atendente`, `cozinha`, `entregador`), `ativo`.
- **Customer**: `id` (UUIDv7), `nome`, `telefone`, `email`, `observacoes`.
- **Address**: `id` (UUIDv7), `customer_id`, `rua`, `numero`, `complemento`, `bairro`, `cidade`, `uf`, `cep`, `referencia`, `padrao`.
- **Category**: `id` (UUIDv7), `nome`, `slug`, `ordem`, `ativa`.
- **Product**: `id` (UUIDv7), `category_id`, `nome`, `slug`, `descricao`, `imagem`, `tipo_atendimento` (`pronta_entrega` ou `sob_encomenda`), `tempo_preparo_min`, `ativo`, `disponivel_na_loja_online`.
- **ProductVariant**: `id` (UUIDv7), `product_id`, `nome` (ex.: `"Único"`, `"Pequeno 15cm"`, `"Caixa com 6"`), `sku`, `preco` (centavos), `estoque_atual`, `estoque_reservado`, `estoque_minimo`, `ordem`, `ativo`. *(Todo produto possui ao menos uma variante padrão; preço, estoque de pronta-entrega e receita vinculam-se à variante).*
- **Material** (insumo): `id` (UUIDv7), `nome`, `unidade` (`g`, `kg`, `ml`, `l`, `un` — convertida para base na persistência), `estoque_atual`, `estoque_reservado`, `estoque_minimo`, `custo_unitario_medio` (centavos), `fornecedor_id`.
- **Supplier**: `id` (UUIDv7), `nome`, `contato`, `telefone`, `email`.
- **Recipe** (ficha técnica): `product_variant_id` ↔ `material_id` com `quantidade` necessária e `rendimento_base`, permitindo calcular o custo unitário exato de cada variante.
- **ProductionBatch** (lote de produção): `id` (UUIDv7), `product_variant_id`, `quantidade_produzida`, `custo_total_insumos` (centavos), `produzido_por` (`user_id`), `criado_em`. *(Converte insumos do estoque de `Material` em saldo de `ProductVariant` para produtos de pronta-entrega).*
- **StockMovement**: `id` (UUIDv7), `target_type` (`material` ou `product_variant`), `target_id`, `tipo` (`entrada_compra`, `saida_producao`, `entrada_producao`, `saida_pedido`, `estorno_cancelamento`, `ajuste`, `perda`), `quantidade`, `custo_unitario` (centavos, opcional para recalcular custo médio ponderado na `entrada_compra`), `motivo`, `order_id` (nullable), `production_batch_id` (nullable), `criado_em`.
- **Order**: `id` (UUIDv7), `order_number` (sequencial amigável), `public_token` (token não adivinhável para acompanhamento), `customer_id`, `origem` (`web`, `balcao`, `telefone`, `whatsapp`), `tipo` (`entrega`, `retirada`), `status`, `subtotal`, `taxa_entrega`, `desconto`, `total`, `forma_pagamento` (`pix_online`, `cartao_entrega`, `dinheiro_entrega`, `pagamento_retirada`), `troco_para` (centavos, nullable), `status_pagamento` (`pendente`, `pago`, `reembolsado`, `expirado`), `observacoes`, `agendado_para` (nullable), `reserva_expira_em` (TTL para Pix online), `criado_em`.
- **OrderItem**: `id` (UUIDv7), `order_id`, `product_id`, `product_variant_id`, `nome_snapshot`, `variante_snapshot`, `preco_snapshot` (centavos), `custo_unitario_snapshot` (centavos — congelado a partir da receita no ato do pedido para relatórios de margem real), `quantidade`, `observacoes`.
- **Delivery**: `id` (UUIDv7), `order_id`, `entregador_id` (`user_id` nullable), `delivery_zone_id`, `endereco_snapshot` (JSONB), `status` (`pendente`, `atribuida`, `em_rota`, `entregue`, `falhou`), `saida_em`, `entregue_em`, `taxa` (centavos).
- **Payment**: `id` (UUIDv7), `order_id`, `metodo` (`pix_online`, `cartao_credito`, `cartao_debito`, `dinheiro`), `status` (`pendente`, `pago`, `expirado`, `estornado`), `valor` (centavos), `txid` / `ref_externa`, `qr_code_payload`, `expira_em`, `pago_em`.
- **DeliveryZone**: `id` (UUIDv7), `nome_bairro`, `cep_inicio`, `cep_fim`, `taxa` (centavos), `tempo_estimado_min`, `ativa`.
- **AuditLog**: `id` (UUIDv7), `user_id`, `acao`, `entidade`, `entidade_id`, `detalhes` (JSONB), `criado_em`.

### Máquina de estados do pedido

```
novo → confirmado → em_preparo → pronto → (saiu_para_entrega → entregue)
                                      └→ (retirado)
qualquer estado (exceto entregue/retirado) → cancelado
```

- Transições válidas definidas **em um único lugar** no `service`, com testes unitários cobrindo todas.
- **Reserva de estoque na criação (`novo`)**:
  - Ao criar o pedido, o estoque necessário é **reservado** transacionalmente (`estoque_reservado` na `ProductVariant` se o produto for `pronta_entrega`, ou nos `Materials` da receita se for `sob_encomenda`).
  - Se `forma_pagamento == pix_online`, o pedido nasce com `reserva_expira_em = now() + 15min`. Um worker em background na API cancela automaticamente pedidos Pix vencidos, liberando a reserva e emitindo evento SSE.
- **Baixa efetiva na confirmação (`confirmado`)**:
  - Quando o pedido passa para `confirmado` (via webhook do Pix ou aceite manual de pedido com pagamento na entrega/balcão), a reserva é convertida em **baixa definitiva de estoque** numa **transação**, gerando registros em `StockMovement`.
- **Estorno em cancelamento (`cancelado`)**:
  - Cancelar um pedido em `novo` libera a reserva; cancelar após `confirmado` gera movimentação de `estorno_cancelamento` devolvendo o saldo ao estoque (ou registrando `perda` se o pedido já estava em preparo/pronto e os itens foram descartados, conforme opção do operador).

### Regras de negócio-chave

1. **Snapshot completo no item do pedido:** `nome_snapshot`, `variante_snapshot`, `preco_snapshot` e `custo_unitario_snapshot` são copiados para `OrderItem` — alterar produto, variante ou custo de insumo no futuro nunca altera pedidos ou margens históricas.
2. **Custo médio ponderado de insumos:** entradas de compra em `StockMovement` atualizam automaticamente o `custo_unitario_medio` do `Material`, recalculando o custo das receitas.
3. **Modelo híbrido de estoque:**
   - Produtos `pronta_entrega` têm saldo controlado na `ProductVariant`, abastecido por `ProductionBatch` (que consome insumos da `Recipe`).
   - Produtos `sob_encomenda` validam/consomem diretamente a disponibilidade dos insumos da `Recipe`.
4. **Alertas e disponibilidade automática:** estoque abaixo do mínimo dispara alerta (`stock.low`) no desktop; variante sem estoque disponível (`estoque_atual - estoque_reservado <= 0`) fica indisponível automaticamente na loja online (configurável).
5. **Horário de funcionamento:** pedido web só é aceito se a loja estiver "aberta" (configuração de horário + flag manual), ou como agendamento se habilitado.
6. **Taxa de entrega e troco:** taxa calculada pela `DeliveryZone` (bairro/faixa de CEP); pagamento em dinheiro na entrega permite informar `troco_para` (validando `troco_para >= total`).

---

## 5. Esquema de banco (resumo)

Crie migrations goose com:

- **Chaves primárias `UUID` (UUIDv7)** em todas as tabelas (geradas via `uuid.NewV7()` em Go), combinadas com `order_number BIGSERIAL` (ou sequência formatada) na tabela `orders` para identificação humana amigável — documentado em ADR.
- `created_at`, `updated_at` (`TIMESTAMPTZ`) em todas as tabelas mutáveis.
- Índices em: `orders(status, created_at DESC)`, `orders(customer_id)`, `orders(status, reserva_expira_em)` (para o worker de expiração), `order_items(order_id)`, `stock_movements(target_type, target_id, created_at DESC)`, `products(category_id)`, `product_variants(product_id)`.
- Constraints: `CHECK (estoque_atual >= 0)`, `CHECK (estoque_reservado >= 0)`, `CHECK (estoque_atual >= estoque_reservado)`, FKs com `ON DELETE` explícito, enums via `CHECK` ou tipo enum do Postgres.
- Seeds de desenvolvimento (categorias, produtos com variantes, materiais, receitas, zonas de entrega e um admin) em script separado.

---

## 6. Contrato da API

Abordagem **Spec-First**: `docs/api/openapi.yaml` é mantido atualizado em cada fase.
Prefixo `/api/v1`. Respostas em JSON.
Paginação padronizada via **`limit/offset`** (`page`, `page_size`) retornando envelope com `data` e `meta: { page, page_size, total_items, total_pages }`.
Erros no formato:

```json
{ "error": { "code": "ORDER_INVALID_TRANSITION", "message": "...", "details": {} } }
```

### Públicas (`/api/v1/public`)

| Método | Rota | Descrição |
|---|---|---|
| GET | `/store` | Dados da loja, horário, métodos de pagamento aceitos, status aberto/fechado |
| GET | `/menu` | Categorias + produtos disponíveis com suas variantes e preços |
| GET | `/products/{slugOrId}` | Detalhe do produto e variantes |
| POST | `/delivery/quote` | Calcula taxa e tempo estimado de entrega por CEP/bairro |
| POST | `/orders` | Cria pedido (rate limit + `Idempotency-Key`, suporta Pix online ou pagamento na entrega/retirada) |
| GET | `/orders/{id}?token=...` | Acompanhar status do pedido e dados do Pix via `public_token` |
| POST | `/payments/pix/webhook` | Webhook do gateway Pix (validar assinatura HMAC, confirmar pagamento e baixar estoque) |

### Administrativas (`/api/v1/admin`, JWT)

| Recurso | Operações |
|---|---|
| `/auth/login`, `/auth/refresh`, `/auth/logout` | Autenticação |
| `/orders` | listar (filtros por status/data/origem/pagamento), detalhe, criar (balcão/telefone), mudar status, cancelar |
| `/orders/stream` | **SSE** com eventos `order.created`, `order.updated`, `order.expired`, `stock.low` |
| `/products`, `/products/{id}/variants`, `/categories` | CRUD + upload de imagem |
| `/materials`, `/suppliers` | CRUD |
| `/recipes` | CRUD da ficha técnica por variante de produto + cálculo de custo/margem sugerida |
| `/production/batches` | listar e registrar lote de produção (baixa insumos → incrementa estoque da variante) |
| `/stock/movements` | listar, registrar entrada de compra (com atualização de custo médio), ajuste ou perda |
| `/customers` | CRUD, endereços, histórico de pedidos |
| `/deliveries`, `/delivery-zones` | CRUD de zonas, listar entregas, atribuir entregador, atualizar status |
| `/reports/sales`, `/reports/top-products`, `/reports/margins`, `/reports/stock` | Relatórios por período (vendas, ranking, margem real baseada em `custo_unitario_snapshot`, estoque) |
| `/users` | CRUD (apenas admin) |
| `/settings` | Horário, taxas, zonas, métodos de pagamento, TTL de reserva Pix, flag aberto/fechado |

---

## 7. Segurança

- Senhas com **argon2id** (ou bcrypt com custo adequado).
- JWT de acesso curto (15 min) + refresh token rotativo armazenado com hash no banco.
- Autorização por papel (RBAC) em middleware (`admin`, `atendente`, `cozinha`, `entregador`).
- **Rate limiting** nas rotas públicas, especialmente `POST /orders` e `/auth/login`.
- CORS restrito às origens conhecidas.
- Validação e sanitização de toda entrada; limite de tamanho de body e de upload (validação de MIME type real da imagem).
- Webhook de pagamento com verificação de assinatura e processamento idempotente.
- Segredos só em variáveis de ambiente; `.env.example` versionado, `.env` no `.gitignore`.
- Headers de segurança e HTTPS obrigatório em produção.
- Logs sem dados sensíveis (sem senha, token, dados completos de pagamento).
- Conformidade básica com **LGPD**: coletar o mínimo, política de privacidade na landing, possibilidade de anonimização/exclusão de dados do cliente.

---

## 8. Desktop (Wails v2 + Vue 3)

### Módulos/telas

1. **Login**
2. **Painel de pedidos** (kanban por status, atualização em tempo real via SSE, som/notificação em novo pedido, badge de pagamento na entrega/troco ou Pix pago)
3. **Novo pedido (balcão/telefone)** (seleção rápida de produtos/variantes, cliente, entrega/retirada e forma de pagamento)
4. **Cozinha (KDS)** (fila de preparo simplificada, tela cheia, destaque para variações e observações)
5. **Entregas** (atribuir entregador, status, endereço, valor a cobrar na entrega e troco)
6. **Produtos, variantes e categorias**
7. **Materiais, estoque e lotes de produção** (alertas de mínimo, entrada de compras com custo, registro de lote produzido)
8. **Receitas (Fichas técnicas)** (insumos por variante, custo calculado automaticamente e simulador de margem)
9. **Clientes e endereços**
10. **Relatórios** (vendas, ranking de produtos/variantes, lucro e margem bruta real, consumo/perdas de estoque)
11. **Configurações, zonas de entrega e usuários**

### Diretrizes

- O Go do Wails atua como **camada fina e segura**: guarda os tokens (access/refresh) no keyring do SO, faz as chamadas à API (renovando o token automaticamente em caso de 401) e expõe métodos tipados ao Vue. Regra de negócio fica na API.
- **Ponte SSE no Go do Wails:** como o JWT fica no processo Go e `EventSource` do browser não suporta header `Authorization`, o backend Go do Wails mantém a conexão SSE com `/api/v1/admin/orders/stream` (com reconexão exponencial automática) e emite eventos para o Vue via `runtime.EventsEmit`.
- Cliente HTTP da API gerado a partir do **OpenAPI** (`oapi-codegen` no Go, `openapi-typescript` no front) para manter contratos sincronizados.
- Recursos nativos: **impressão de comanda** (térmica 80mm ESC/POS ou PDF), notificações nativas do sistema, atalhos de teclado.
- Indicador visual de status da conexão em tempo real no topo/rodapé do app.
- Gerar instalador para Windows (prioridade), macOS e Linux.

---

## 9. Landing / Loja online (Nuxt 3)

### Páginas

- **Home** (hero, destaques, sobre, horário, contato, mapa)
- **Cardápio** (categorias, busca, filtros de pronta-entrega vs. encomenda)
- **Produto** (detalhe, seleção de variante/tamanho/sabor, observações, quantidade)
- **Carrinho** e **Checkout** (identificação, endereço com cálculo de taxa ou retirada, agendamento, escolha entre **Pix online** com QR Code/copia-e-cola e timer de expiração ou **Pagamento na entrega/retirada** com opção de troco)
- **Acompanhar pedido** (via link `/pedidos/{id}?token=...` com atualização de status)
- **Política de privacidade / Termos**

### Diretrizes

- SSR/SSG com **SEO** (meta tags, Open Graph, `sitemap.xml`, dados estruturados `LocalBusiness`/`Menu`).
- **Mobile-first**, performance (Lighthouse ≥ 90), acessibilidade (WCAG AA).
- Carrinho persistido no `localStorage` com validação de preços/estoque no checkout.
- Se a loja estiver fechada, mostrar aviso claro e permitir agendamento (se habilitado).
- Estados de loading/erro/vazio bem tratados.

---

## 10. Estrutura de repositório (monorepo)

```
doceria/
├── README.md
├── docs/
│   ├── architecture.md
│   ├── adr/                    # Architecture Decision Records
│   ├── diagrams/               # Mermaid/PlantUML/C4
│   └── api/openapi.yaml        # Contrato OpenAPI 3 (Spec-First)
├── api/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── config/
│   │   ├── http/               # router, middlewares, handlers, código gerado oapi-codegen
│   │   ├── service/            # regras de negócio + worker de expiração de reservas
│   │   ├── repository/         # código gerado pelo sqlc + wrappers transacionais
│   │   ├── domain/             # entidades, erros, máquina de estados
│   │   ├── payment/            # interface + implementação Pix + fake
│   │   ├── realtime/           # broker SSE
│   │   └── platform/           # db, logger, storage
│   ├── db/
│   │   ├── migrations/
│   │   ├── queries/            # .sql do sqlc
│   │   └── seeds/
│   ├── sqlc.yaml
│   └── Dockerfile
├── desktop/
│   ├── main.go
│   ├── app.go
│   ├── internal/               # cliente API, keyring, ponte SSE → Wails Events, impressora
│   ├── wails.json
│   └── frontend/               # Vue 3 + TS
├── web/                        # Nuxt 3
├── docker-compose.yml          # postgres + api (+ mailhog/minio se usar)
├── Makefile                    # atalhos: dev, test, migrate, generate, lint
└── .github/workflows/          # CI
```

---

## 11. Passo a passo de implementação (fases)

> **Instrução para a IA:** execute **uma fase por vez**. Ao final de cada fase, liste o que foi feito, como rodar/testar e aguarde minha confirmação antes de seguir. Se houver ambiguidade, **pergunte** em vez de assumir.

### Fase 0 — Fundação
1. Criar estrutura do monorepo, `.gitignore`, `Makefile`, `docker-compose.yml` (Postgres).
2. Criar `docs/adr/` com os primeiros ADRs:
   - ADR-001: Arquitetura cliente-servidor com API central e Wails v2 no desktop (com ponte SSE em Go).
   - ADR-002: Acesso a dados com `sqlc` + `pgx` vs. ORM.
   - ADR-003: Identificadores `UUIDv7` como chave primária + `order_number` sequencial em pedidos.
   - ADR-004: Modelo híbrido de estoque (lotes de produção para pronta-entrega + insumos para encomendas) e reserva com TTL no checkout.
   - ADR-005: Contrato OpenAPI Spec-First e paginação `limit/offset`.
3. Criar esqueleto inicial do `docs/api/openapi.yaml` (healthcheck + estrutura padrão de erro e paginação).
4. Configurar lint (`golangci-lint`, ESLint, Prettier) e workflows básicos de CI no GitHub Actions.

### Fase 1 — API: base
1. Estrutura de pastas, config por env, logger `slog`, health check (`/healthz`).
2. Conexão com Postgres (pgx pool), runner de migrations `goose`, `sqlc` e geração OpenAPI configurados.
3. Middlewares: request ID, logging estruturado, recover, CORS, rate limit.
4. Padrão de erros de domínio mapeados para HTTP e respostas JSON padronizadas.
5. Testes de integração base com `testcontainers-go`.

### Fase 2 — API: autenticação e usuários
1. Atualizar `openapi.yaml` com endpoints de `/auth` e `/users`.
2. Tabela `users`, hash de senha (`argon2id`), login, refresh token rotativo, logout.
3. Middleware JWT + RBAC por papel (`admin`, `atendente`, `cozinha`, `entregador`).
4. Seed do primeiro usuário `admin`.
5. Testes de integração do fluxo completo de autenticação e autorização.

### Fase 3 — API: catálogo (categorias, produtos e variantes)
1. Atualizar `openapi.yaml` com endpoints públicos e administrativos de catálogo.
2. CRUD de categorias, produtos e variantes (`ProductVariant`), upload de imagem (interface `Storage` com implementação local e S3 compatível).
3. Endpoints públicos `GET /store`, `GET /menu` e `GET /products/{slugOrId}`.
4. Testes de integração.

### Fase 4 — API: materiais, receitas, lotes de produção e estoque
1. Atualizar `openapi.yaml` com endpoints de materiais, fornecedores, receitas, lotes e estoque.
2. CRUD de materiais e fornecedores.
3. Receitas (ficha técnica) por variante de produto e cálculo automático de custo unitário.
4. Registro de `ProductionBatch` (transação que consome insumos da receita e abastece o estoque da variante de pronta-entrega).
5. Movimentações de estoque (`StockMovement`), cálculo de custo médio ponderado na entrada de compras e alerta de estoque mínimo.
6. Testes de consistência transacional (saldo disponível nunca negativo).

### Fase 5 — API: pedidos, reservas e tempo real (SSE)
1. Atualizar `openapi.yaml` com endpoints de pedidos e SSE.
2. Modelo de pedido (`Order`), itens com snapshots completos (`preco_snapshot` e `custo_unitario_snapshot`), máquina de estados com testes unitários exaustivos.
3. Criação pública (`Idempotency-Key`, reserva de estoque, suporte a Pix online com TTL de 15 min e pagamento na entrega/retirada com troco) e criação via balcão/telefone.
4. Conversão de reserva em baixa efetiva na confirmação, estorno em cancelamento e worker de expiração de reservas Pix vencidas.
5. **SSE** (`/orders/stream`) com broker em memória (interface preparada para Redis/Postgres `LISTEN/NOTIFY`).
6. Testes de concorrência (múltiplos pedidos disputando o último item em estoque simultaneamente).

### Fase 6 — API: entregas, clientes, pagamentos e relatórios
1. Atualizar `openapi.yaml` finalizando todos os contratos da API.
2. Zonas e cotação de taxa de entrega (`/delivery/quote`), atribuição de entregador e evolução de status da entrega.
3. Gestão de clientes, endereços e histórico de pedidos.
4. Interface `PaymentProvider` + implementação fake para dev/testes + implementação Pix com geração de QR Code e webhook autenticado.
5. Endpoints de relatórios (vendas, produtos/variantes mais vendidos, margem bruta histórica real e posição de estoque).

### Fase 7 — Desktop (Wails v2 + Vue 3)
1. Inicializar projeto Wails v2 com Vue 3 + TypeScript; configurar Tailwind, Pinia e Vue Router.
2. Gerar tipos/cliente da API a partir do `openapi.yaml`; implementar armazenamento seguro do token no keyring do SO e ponte SSE no Go emitindo eventos Wails.
3. Login e layout base (sidebar por papel, tema, indicador de conexão SSE, atalhos).
4. Painel de pedidos (kanban) em tempo real + notificação sonora + tela da Cozinha (KDS).
5. Demais módulos na ordem: novo pedido (balcão) → produtos/variantes → materiais/estoque/lotes → receitas → entregas → clientes → relatórios → configurações.
6. Impressão de comanda (80mm / PDF).
7. Testes de componentes (Vitest) e build do instalador.

### Fase 8 — Landing / Loja (Nuxt 3)
1. Setup Nuxt 3 + Tailwind + Pinia + cliente tipado gerado do `openapi.yaml`.
2. Home, cardápio com filtro de categorias, página de produto com seleção de variante.
3. Carrinho persistido e Checkout (cálculo de entrega por CEP/bairro, agendamento, escolha entre Pix online com QR Code/timer ou pagamento na entrega com troco).
4. Página pública de acompanhamento de pedido em tempo real/polling via `public_token`.
5. SEO, acessibilidade, performance e testes E2E (Playwright) cobrindo o fluxo completo de compra.

### Fase 9 — Qualidade e produção
1. Cobertura de testes mínima verificada no CI (≥ 70% nos services).
2. Observabilidade: logs estruturados, métricas (`/metrics` Prometheus), health/readiness checks.
3. Dockerfile multi-stage da API, configuração de deploy com HTTPS e rotina de backup do Postgres.
4. Deploy da landing e pipeline de CI/CD completo (lint, testes, build de imagem Docker e release dos instaladores desktop).

### Fase 10 — Polimento para portfólio
1. README completo com visão geral, arquitetura, decisões e trade-offs, GIFs/screenshots e instruções `make dev`.
2. Diagramas C4 (contexto e contêineres) e diagrama ER em `docs/diagrams/`.
3. Seção "Possíveis evoluções" (modo offline com SQLite + sync, multi-loja, app do entregador, integração oficial WhatsApp Cloud API).

---

## 12. Convenções de código

- **Go:** `gofmt`, `golangci-lint`, erros com `fmt.Errorf("...: %w", err)`, contexto (`context.Context`) em todas as chamadas de I/O, sem variáveis globais mutáveis, interfaces definidas por quem consome.
- **TypeScript:** `strict: true`, sem `any` implícito, Composition API com `<script setup>`.
- **Commits:** Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
- **Branches:** `main` protegida; trabalho em `feat/*` com PRs, mesmo sendo projeto solo (histórico limpo vale para portfólio).
- **Nomes:** código, tabelas, colunas e identificadores em **inglês**; textos de interface, comentários de domínio e documentação em **português**.

---

## 13. Definição de pronto (para cada fase)

- [ ] Código compila e passa no lint
- [ ] Sub-agente dedicado de QA/Testes (`qa-tester`) executado e aprovado na fase (testes unitários, integração, edge cases e concorrência aplicáveis)
- [ ] Testes escritos e passando
- [ ] Migrations aplicam (`up`) e revertem (`down`) sem erro
- [ ] `docs/api/openapi.yaml` e código gerado atualizados
- [ ] Instruções claras de como rodar e validar a fase
- [ ] Nenhum segredo commitado

---

## 14. Como a IA deve responder

- Antes de codar cada fase, apresente um **plano curto** (arquivos que serão criados/alterados).
- Ao final da implementação de cada fase, acione um **sub-agente dedicado 100% a testes** (`qa-tester`) para auditar, escrever/expandir testes e validar a cobertura antes de entregar a fase ao usuário.
- Entregue código **completo e executável**, não trechos soltos.
- Explique decisões não óbvias em 1–2 frases.
- Aponte riscos e trade-offs quando houver alternativas razoáveis.
- Não adicione funcionalidades fora do escopo sem perguntar.
- Se uma tecnologia citada tiver mudado, **verifique a documentação oficial** e avise antes de seguir.
