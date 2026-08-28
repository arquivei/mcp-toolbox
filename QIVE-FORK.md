# Fork Qive do MCP Toolbox

Fork de [`googleapis/mcp-toolbox`](https://github.com/googleapis/mcp-toolbox) que
adiciona `allowedTables` ao source BigQuery: allowlist em nível de tabela, para
expor tabelas específicas de um dataset sem liberar o dataset inteiro.

Consumido por `gitlab.com/arquivei/staffs/tools/google-toolbox-mcp`, que builda a
imagem de produção a partir daqui. Desenho e justificativa:
`docs/superpowers/specs/2026-08-25-allowed-tables-toolbox-fork-design.md` naquele repo.

## Estrutura de refs

- **`qive/main`** — a linha viva. Rebaseada em cada release novo do upstream,
  portanto **force-pushed**. Nunca referencie esta branch num build.
- **`vX.Y.Z-qive.N`** — tags imutáveis, uma por rebase. É o que o build ancora,
  sempre por SHA.

## Rebase para uma versão nova do upstream

```bash
git fetch upstream --tags
git rebase v1.10.0
go build ./... && go test ./internal/sources/bigquery/... ./internal/tools/bigquery/...
git tag v1.10.0-qive.1
git push --force-with-lease origin qive/main
git push origin v1.10.0-qive.1
```

Depois, atualizar `TOOLBOX_REF` no `Dockerfile` do repo de deploy com o SHA da tag nova.

## Onde o patch mexe

- `internal/sources/bigquery/bigquery.go` — campo `allowedTables`, parse, e os
  predicados `IsTableAllowed` / `IsDatasetVisible` / `BigQueryAllowedTables`.
- `internal/tools/bigquery/*` — as 7 ocorrências do gate
  `if len(source.BigQueryAllowedDatasets()) > 0` passam a considerar as duas
  listas, e os call sites passam a validar em nível de tabela.
- `internal/tools/bigquery/bigquerycommon/` — `util.go` (descrições) e
  `mock_source.go` (que não é `_test.go` e portanto precisa implementar a
  interface).

## Invariantes que os testes protegem

- Config **só** com `allowedTables` mantém a validação ligada. Se os gates
  voltarem a olhar apenas `BigQueryAllowedDatasets()`, a configuração mais
  restritiva vira a mais permissiva.
- Referência wildcard (`dataset.prefix_*`) é recusada quando dependeria de uma
  entrada de tabela, e aceita quando o dataset está em `allowedDatasets`.
- `BigQueryAllowedTables()` devolve ordem estável — a descrição das tools entra
  no payload de `tools/list` e é objeto de assertion no repo de deploy.
