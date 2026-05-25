# tsure

`tsure` e um ERP em construcao para gestao de leasing e locacao de materiais
para eventos. O foco do produto e conectar comercial, operacao, frota,
inventario e financeiro em um fluxo unico de ordem de servico.

## Monorepo Layout

```text
.
├── apps/
│   └── web/        # App web em Go com HTML server-side e HTMX
│   └── mobile/     # Template Android nativo em Kotlin
├── database/       # Schema e seed SQL
├── docs/           # Escopo funcional, backlog, API e mapas de tela
├── docker-compose.yml
├── go.mod
└── README.md
```

O frontend inicial usa HTML renderizado no servidor com HTMX para atualizacao
progressiva do painel de ordens de servico.

O monorepo agora tambem inclui uma base mobile Android em Kotlin para evoluir
rotinas de campo, checklists, apontamento de equipe e consulta rapida de OS.

## Requirements

- Go 1.22+
- PostgreSQL 18+

## Run PostgreSQL

Start a local database with Docker:

```bash
docker compose up -d postgres
```

## Configure

Default connection settings:

```text
DATABASE_URL=postgres://tsure:tsure@127.0.0.1:5432/tsure?sslmode=disable
ADDR=127.0.0.1:3456
```

## Run the app

```bash
go run ./apps/web
```

Open `http://127.0.0.1:3456`.

## Estado Atual do App

- `GET /` carrega o shell principal do `tsure`
- `GET /orders` renderiza painel de ordens de servico
- `POST /orders` cria uma nova OS em status `orcamento`
- `PUT /orders/{id}` avanca a OS pelo fluxo operacional
- `DELETE /orders/{id}` remove a OS da base

Na primeira execucao, a aplicacao cria a tabela `service_orders` e injeta
alguns registros de exemplo ligados ao dominio de eventos, frota e recebiveis.

## Mobile

O app Android base fica em `apps/mobile` e foi deixado propositalmente simples
como template inicial. Ele abre uma dashboard estatica e uma CTA para futuras
acoes de campo.

Arquivos principais:

- `apps/mobile/settings.gradle.kts`
- `apps/mobile/app/build.gradle.kts`
- `apps/mobile/app/src/main/java/com/tsure/mobile/MainActivity.kt`

Para abrir no Android Studio, importe a pasta `apps/mobile`.

## Escopo de Produto

O repositorio deixa de ser template e passa a assumir explicitamente o produto
`tsure`, com base nestes modulos:

- comercial e orcamentos;
- ordens de servico e agenda;
- frota e inventario;
- financeiro de recebiveis;
- base para RH, fiscal e auditoria.

Os documentos de produto e arquitetura estao em [docs/README.md](docs/README.md).
