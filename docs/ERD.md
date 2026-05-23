# ERD — Novo Sistema (tsure)

Modelo redesenhado a partir da análise do banco legado Radelgo (`database-access/exports/`). PostgreSQL, UUIDv7 em todos os PKs, auditoria padrão (`created_at`, `updated_at`, `deleted_at`).

## Decisões de redesenho

- `TabEscala` + `TabEscalaRet` → **`agenda_equipe`** com `papel` enum (`instalacao` | `retorno`). Elimina duplicação estrutural.
- `TabSaidaVeic` + `TabSaidaVeicRet` → **`agenda_veiculos`** consolidado, uma linha por uso, com `km_saida` / `km_retorno` e motorista.
- `TabEquipSaida` → **`movimentacoes_estoque`** enriquecida com `tipo` (`saida` | `retorno` | `avaria` | `perda` | `baixa`), virando o livro-razão do estoque físico.
- `TabFotos` → **`anexos`** polimórfico (`entidade` + `registro_id`), aceita arquivos em qualquer entidade, campo `url` apontando para S3.
- `agenda` monolítica (42 colunas) → slim. **Endereços e contatos do evento extraídos para `agenda_locais` (1:N) e `agenda_local_contatos` (1:N por local)** — um evento pode ocorrer em múltiplos locais (montagem + apresentação + apoio), e cada local pode ter seus próprios contatos; status sai para `agenda_status_historico`; cancelamento referenciado em `motivos_cancelamento`.
- `TabPgto` plano → **`contas_receber`** (cabeçalho com saldo) + **`recebimentos`** (baixas). Suporta parcelas, baixa parcial e renegociação.
- Status com **ENUM nativo PostgreSQL** + tabela de histórico de transições.
- `TabKit` → **`kit_composicao`** com unique (`servico_locacao_id`, `equipamento_id`).
- Descartados: `FanalOper` (stub vazio), `CadDepto` / `CadSetor` (legado de outro ERP), `CadSys` → substituído por `parametros_sistema` key-value.
- `CadFunc.NomFunc` etc. expandidos para `nome`, `data_nascimento`, etc. (sem abreviações, sem `Func`/`Tab`/`Cad`).

## Enums PostgreSQL

```sql
CREATE TYPE cliente_tipo          AS ENUM ('pessoa_fisica', 'pessoa_juridica');
CREATE TYPE funcionario_status    AS ENUM ('ativo', 'desligado', 'afastado');
CREATE TYPE usuario_papel         AS ENUM ('admin', 'comercial', 'operacao', 'financeiro', 'fiscal', 'campo');
CREATE TYPE agenda_status         AS ENUM ('orcamento', 'em_analise', 'aprovado', 'agendado', 'em_execucao', 'aguardando_retorno', 'finalizado', 'cancelado');
CREATE TYPE agenda_tipo_evento    AS ENUM ('particular', 'licitacao', 'cortesia', 'recorrente');
CREATE TYPE agenda_tipo_retorno   AS ENUM ('mesma_equipe', 'outra_equipe');
CREATE TYPE agenda_local_tipo     AS ENUM ('principal', 'montagem', 'apoio', 'hospedagem', 'estacionamento');
CREATE TYPE agenda_equipe_papel   AS ENUM ('instalacao', 'retorno');
CREATE TYPE veiculo_status        AS ENUM ('disponivel', 'reservado', 'em_rota', 'em_manutencao', 'indisponivel');
CREATE TYPE equipamento_status    AS ENUM ('disponivel', 'em_uso', 'em_manutencao', 'baixado');
CREATE TYPE movimentacao_tipo     AS ENUM ('saida', 'retorno', 'avaria', 'perda', 'baixa', 'manutencao');
CREATE TYPE forma_pagamento       AS ENUM ('dinheiro', 'cheque', 'transferencia', 'pix', 'boleto', 'cartao');
CREATE TYPE tipo_documento_fiscal AS ENUM ('recibo', 'nota_fiscal', 'cupom');
CREATE TYPE conta_status          AS ENUM ('previsto', 'faturado', 'em_aberto', 'parcial', 'pago', 'renegociado', 'cancelado');
```

---

## ERD — Núcleo Operacional

```mermaid
erDiagram
    clientes ||--o{ agenda : contrata
    usuarios ||--o{ agenda : registra
    funcionarios ||--o| usuarios : possui_acesso
    motivos_cancelamento ||--o{ agenda : justifica
    agenda ||--o{ agenda_itens : detalha
    agenda ||--o{ agenda_locais : ocorre_em
    agenda_locais ||--o{ agenda_local_contatos : tem_contatos
    agenda ||--o{ agenda_equipe : aloca_equipe
    agenda ||--o{ agenda_veiculos : aloca_veiculos
    agenda ||--o{ agenda_status_historico : transita
    funcionarios ||--o{ agenda_equipe : escalado
    funcionarios ||--o{ agenda_veiculos : dirige
    veiculos ||--o{ agenda_veiculos : usado

    clientes {
        uuid id PK
        cliente_tipo tipo
        varchar nome_razao_social
        varchar nome_fantasia
        varchar documento UK "CPF ou CNPJ"
        varchar email
        varchar telefone_fixo
        varchar telefone_celular
        varchar logradouro
        varchar numero
        varchar complemento
        varchar bairro
        varchar cidade
        char uf
        varchar cep
        boolean bloqueado
        varchar motivo_bloqueio
        text observacoes
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    funcionarios {
        uuid id PK
        varchar nome
        varchar documento UK
        date data_nascimento
        date data_admissao
        date data_desligamento
        varchar motivo_desligamento
        varchar cargo
        varchar centro_custo
        varchar telefone
        varchar email
        varchar logradouro
        varchar numero
        varchar bairro
        varchar cidade
        char uf
        varchar cep
        funcionario_status status
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    usuarios {
        uuid id PK
        uuid funcionario_id FK "nullable"
        varchar login UK
        varchar email UK
        varchar senha_hash
        varchar nome
        usuario_papel papel
        boolean ativo
        timestamptz ultimo_acesso
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    veiculos {
        uuid id PK
        varchar placa UK
        varchar descricao
        varchar marca
        varchar modelo
        smallint ano_fabricacao
        smallint ano_modelo
        varchar chassi
        varchar renavam
        varchar combustivel
        varchar cnpj_proprietario
        varchar numero_apolice_vigente
        date data_aquisicao
        decimal km_atual
        veiculo_status status
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    agenda {
        uuid id PK
        bigint numero UK "sequencial"
        uuid cliente_id FK
        uuid usuario_registro_id FK
        uuid usuario_aprovador_id FK
        uuid motivo_cancelamento_id FK
        agenda_status status
        agenda_tipo_evento tipo_evento
        agenda_tipo_retorno tipo_retorno
        varchar descricao_evento
        date data_evento
        time hora_evento
        date data_instalacao
        time hora_instalacao
        date data_retorno_prevista
        date data_retorno_real
        forma_pagamento forma_pagamento
        decimal valor_total
        decimal valor_desconto
        decimal valor_liquido
        varchar numero_aprovacao
        timestamptz data_aprovacao
        timestamptz data_cancelamento
        text observacoes
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    agenda_locais {
        uuid id PK
        uuid agenda_id FK
        agenda_local_tipo tipo "principal|montagem|apoio|hospedagem"
        varchar apelido "ex Galpao A, Palco Principal"
        varchar logradouro
        varchar numero
        varchar complemento
        varchar bairro
        varchar cidade
        char uf
        varchar cep
        varchar ponto_referencia
        decimal localizacao_latitude
        decimal localizacao_longitude
        decimal distancia_km
        boolean principal "1 por agenda"
        int ordem
        text observacoes
        timestamptz created_at
        timestamptz updated_at
    }

    agenda_local_contatos {
        uuid id PK
        uuid agenda_local_id FK
        varchar nome
        varchar cargo "produtor|sindico|responsavel"
        varchar telefone_principal
        varchar telefone_secundario
        varchar email
        boolean principal "1 por local"
        text observacoes
        timestamptz created_at
        timestamptz updated_at
    }

    agenda_itens {
        uuid id PK
        uuid agenda_id FK
        uuid servico_locacao_id FK
        int numero_sequencial
        text descricao_complemento
        decimal quantidade
        varchar unidade "DIARIA | UNIDADE | METRO"
        decimal valor_unitario
        decimal valor_desconto
        decimal valor_total
        text observacoes
    }

    agenda_equipe {
        uuid id PK
        uuid agenda_id FK
        uuid funcionario_id FK
        agenda_equipe_papel papel
        timestamptz data_inicio
        timestamptz data_fim
        text observacoes
    }

    agenda_veiculos {
        uuid id PK
        uuid agenda_id FK
        uuid veiculo_id FK
        uuid motorista_id FK
        decimal km_saida
        decimal km_retorno
        timestamptz data_saida
        timestamptz data_retorno
        text observacoes
    }

    agenda_status_historico {
        uuid id PK
        uuid agenda_id FK
        uuid usuario_id FK
        agenda_status status_anterior
        agenda_status status_novo
        text motivo
        timestamptz ocorreu_em
    }

    motivos_cancelamento {
        uuid id PK
        varchar codigo UK
        varchar descricao
        boolean ativo
    }
```

---

## ERD — Catálogo e Estoque

```mermaid
erDiagram
    classificacoes_servico ||--o{ servicos_locacao : classifica
    servicos_locacao ||--o{ agenda_itens : referenciado
    servicos_locacao ||--o{ kit_composicao : composto_de
    equipamentos ||--o{ kit_composicao : compoe
    equipamentos ||--o{ movimentacoes_estoque : movimentado
    agenda ||--o{ movimentacoes_estoque : origina
    usuarios ||--o{ movimentacoes_estoque : registra
    usuarios ||--o{ kit_composicao : monta

    classificacoes_servico {
        uuid id PK
        varchar codigo UK
        varchar descricao "Palco|Tenda|Sonorizacao|Iluminacao|..."
        int ordem
        boolean ativo
    }

    servicos_locacao {
        uuid id PK
        uuid classificacao_id FK
        varchar codigo UK
        varchar descricao
        varchar unidade_padrao "DIARIA | UNIDADE"
        decimal valor_referencia
        boolean ativo
        timestamptz created_at
        timestamptz updated_at
    }

    equipamentos {
        uuid id PK
        varchar codigo_patrimonio UK
        varchar descricao
        varchar marca
        varchar modelo
        varchar numero_serie
        decimal valor_aquisicao
        date data_aquisicao
        int quantidade_total
        int quantidade_disponivel
        equipamento_status status
        text observacoes
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    kit_composicao {
        uuid id PK
        uuid servico_locacao_id FK
        uuid equipamento_id FK
        uuid usuario_cadastro_id FK
        int quantidade
        timestamptz created_at
    }

    movimentacoes_estoque {
        uuid id PK
        uuid agenda_id FK "nullable"
        uuid equipamento_id FK
        uuid responsavel_id FK
        movimentacao_tipo tipo
        int quantidade
        timestamptz data_movimentacao
        text observacoes
    }
```

---

## ERD — Financeiro, Anexos e Governança

```mermaid
erDiagram
    agenda ||--o{ contas_receber : fatura
    clientes ||--o{ contas_receber : titular
    contas_receber ||--o{ recebimentos : baixa_com
    usuarios ||--o{ recebimentos : registra
    usuarios ||--o{ anexos : envia
    usuarios ||--o{ logs_auditoria : gera
    usuarios ||--o{ parametros_sistema : atualiza

    contas_receber {
        uuid id PK
        uuid agenda_id FK
        uuid cliente_id FK
        varchar numero_titulo UK
        char competencia "YYYY-MM"
        date data_emissao
        date data_vencimento
        decimal valor_original
        decimal valor_baixado
        decimal saldo
        conta_status status
        text observacoes
        timestamptz created_at
        timestamptz updated_at
    }

    recebimentos {
        uuid id PK
        uuid conta_receber_id FK
        uuid usuario_registro_id FK
        date data_recebimento
        decimal valor_recebido
        forma_pagamento forma_pagamento
        tipo_documento_fiscal tipo_documento
        varchar numero_documento
        varchar referencia
        text observacoes
        timestamptz created_at
    }

    anexos {
        uuid id PK
        uuid usuario_envio_id FK
        varchar entidade "polimorfico: agenda|cliente|equipamento|..."
        uuid registro_id
        varchar nome_arquivo
        varchar url "S3 URL"
        varchar tipo_mime
        bigint tamanho_bytes
        text descricao
        timestamptz created_at
    }

    parametros_sistema {
        uuid id PK
        uuid usuario_atualizacao_id FK
        varchar chave UK
        text valor
        text descricao
        timestamptz updated_at
    }

    logs_auditoria {
        uuid id PK
        uuid usuario_id FK
        varchar entidade
        uuid registro_id
        varchar acao "create|update|delete|status_change|login"
        jsonb valor_anterior
        jsonb valor_novo
        inet ip_origem
        varchar user_agent
        timestamptz ocorreu_em
    }
```

---

## Mapa Legado → Novo

| Legado                      | Novo                            | Mudança principal                                        |
|-----------------------------|---------------------------------|----------------------------------------------------------|
| `TabClientes`               | `clientes`                      | Endereço inline mantido; documento UK; soft-delete       |
| `CadFunc`                   | `funcionarios`                  | Colunas expandidas; status como enum                     |
| `CadUsuario`                | `usuarios`                      | FK opcional para `funcionarios`; papel como enum         |
| `TabVeiculo`                | `veiculos`                      | `ano_fabricacao`/`ano_modelo` separados; status como enum|
| `TabAgenda`                 | `agenda` + `agenda_status_historico` + `agenda_locais` + `agenda_local_contatos` | 42 colunas slim; endereços/GPS extraídos como 1:N (multi-local); contatos do evento como 1:N por local; histórico fora; cancelamento por FK |
| `TabAgendaItens`            | `agenda_itens`                  | FK explícita para `servicos_locacao`                     |
| `TabEscala` + `TabEscalaRet`| `agenda_equipe`                 | Tabela única com `papel` enum                            |
| `TabSaidaVeic` + `TabSaidaVeicRet` | `agenda_veiculos`        | Tabela única, uma linha por uso completo, com motorista  |
| `TabEquipSaida`             | `movimentacoes_estoque`         | Vira livro-razão com `tipo` (saida/retorno/avaria/...)   |
| `TabFotos`                  | `anexos`                        | Polimórfico; campo `url` S3-compatible                   |
| `TabKit`                    | `kit_composicao`                | Unique composto; clareza semântica                       |
| `CadLocacao`                | `servicos_locacao`              | FK para `classificacoes_servico` (era texto solto)       |
| `CadClass`                  | `classificacoes_servico`        | Tabela lookup formal                                     |
| `TabEquip`                  | `equipamentos`                  | `quantidade_total`/`quantidade_disponivel` explícitos    |
| `TabPgto`                   | `contas_receber` + `recebimentos` | Cabeçalho + baixas; suporta parcelas                   |
| `TabMotCanc`                | `motivos_cancelamento`          | Mantido como lookup                                      |
| `TabCidade` + `CadUF`       | dropadas                        | Cidade/UF inline nos endereços + lista de UFs no app    |
| `CadDepto` + `CadSetor`     | dropadas                        | Legado de outro ERP, sem uso real                        |
| `CadSys`                    | `parametros_sistema`            | Key-value genérico                                       |
| `FanalOper`                 | dropada                         | Stub vazio no legado                                     |
| —                           | `logs_auditoria`                | Trilha de auditoria universal (novo)                     |
