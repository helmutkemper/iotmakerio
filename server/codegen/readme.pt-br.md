# Projects e Templates — Guia do IoTMaker Portal

## Sumário

- [Projects](#projects)
	- [O que é um project?](#o-que-é-um-project)
	- [Ciclo de vida](#ciclo-de-vida-do-project)
	- [Arquivos do project](#arquivos-do-project)
	- [Versões de código](#versões-de-código)
	- [Publicação](#publicação)
	- [Card do marketplace](#card-do-marketplace)
	- [API de projects](#api-de-projects)
- [Templates](#templates)
	- [O que é um template?](#o-que-é-um-template)
	- [Quem cria um template?](#quem-cria-um-template)
	- [Estrutura do ZIP](#estrutura-do-zip)
	- [O manifesto template.json](#o-manifesto-templatejson)
	- [A pasta devices/](#a-pasta-devices)
	- [A pasta output/](#a-pasta-output)
	- [Ciclo de vida do template](#ciclo-de-vida-do-template)
	- [Usando um template na IDE](#usando-um-template-na-ide)
	- [Visibilidade](#visibilidade-do-template)
	- [API de templates](#api-de-templates)
- [Diferença entre project e template](#diferença-entre-project-e-template)

---

## Projects

### O que é um project?

Um **project** é o trabalho de um usuário dentro do portal. É onde o desenvolvedor escreve o código Go de um device customizado, documenta, adiciona imagens e, quando estiver pronto, publica para a comunidade.

Todo project pertence a um único usuário e tem visibilidade **pública** ou **privada**.

---

### Ciclo de vida do project

```
Criação (privado)
      │
      ▼
Desenvolvimento
  ├─ escreve código Go no Monaco editor
  ├─ faz upload de imagens
  ├─ escreve readme.md com frontmatter
  └─ salva versões do código
      │
      ▼
Publicação (opcional)
  ├─ muda visibility para "public"
  ├─ ativa PublishToFeed   → aparece no feed da comunidade
  ├─ ativa PublishToSearch → aparece na busca do marketplace
  └─ ativa ReadyToUse      → recebe badge de qualidade
```

> Os flags de publicação (`PublishToFeed`, `PublishToSearch`, `ReadyToUse`) só têm efeito quando a visibilidade é `public`. Ao tornar o project privado novamente, os três flags são zerados automaticamente pelo servidor.

---

### Arquivos do project

Os arquivos são organizados em três seções:

| Seção  | Conteúdo                | Observação                              |
|--------|-------------------------|-----------------------------------------|
| `code` | Arquivo `.go` do device | Um único arquivo de código Go           |
| `img`  | Imagens do project      | JPG, PNG, etc.                          |
| `docs` | Arquivos Markdown       | `readme.md` é protegido contra exclusão |

O arquivo `readme.md` tem papel especial: ele alimenta o **card do marketplace** via YAML frontmatter. Toda vez que o `readme.md` é salvo, o servidor extrai automaticamente os campos do frontmatter e atualiza o card no banco de dados.

---

### Versões de código

O Monaco editor salva versões numeradas do código Go. Cada `POST /files/code/versions` cria um snapshot com número de versão crescente. Isso permite ao desenvolvedor voltar a uma versão anterior do código quando necessário.

```
versão 1 — commit inicial
versão 2 — adicionou método Run()
versão 3 — corrigiu bug no Init()   ← atual
```

---

### Publicação

Para publicar um project, o fluxo é:

1. Mudar `visibility` para `"public"` via `PUT /api/v1/projects/:id`
2. Preencher o `readme.md` com frontmatter completo (título, descrição, palavras-chave)
3. Ativar os flags desejados no modal de propriedades:
	- **PublishToFeed** — o card aparece nos feeds da comunidade
	- **PublishToSearch** — o project aparece na busca do marketplace
	- **ReadyToUse** — badge visual indicando que o project está maduro e documentado

> Um project recém-criado nunca tem os flags de publicação ativos. O desenvolvedor os ativa manualmente quando sentir que o trabalho está pronto.

---

### Card do marketplace

O card é gerado automaticamente a partir do frontmatter do `readme.md`:

```yaml
---
title: "Sensor APDS9960"
image: "cover.png"
description: "Driver completo para o sensor de gestos APDS9960 via I2C."
keywords: "i2c, sensor, gestos, proximidade"
category: sensors
subcategory: sensors-optical
---
```

| Campo do frontmatter | Campo no banco     | Onde aparece                    |
|----------------------|--------------------|---------------------------------|
| `title`              | `card_title`       | Título do card no feed          |
| `image`              | `card_image`       | Imagem de capa do card          |
| `description`        | `card_description` | Texto resumido (máx. 500 chars) |
| `keywords`           | `card_keywords`    | Tags de busca                   |
| `category`           | `category_id`      | Filtro de categoria             |
| `subcategory`        | `subcategory_id`   | Filtro de subcategoria          |

---

### API de projects

```
GET    /api/v1/projects                        — lista os projects do usuário logado
POST   /api/v1/projects                        — cria um novo project
PUT    /api/v1/projects/:id                    — atualiza nome, visibilidade e flags
DELETE /api/v1/projects/:id                    — exclui o project e todos os arquivos

GET    /api/v1/projects/:id/files              — lista todos os arquivos por seção

GET    /api/v1/projects/:id/files/code         — retorna o código atual + lista de versões
POST   /api/v1/projects/:id/files/code         — faz upload de um arquivo .go
DELETE /api/v1/projects/:id/files/code         — remove o arquivo de código
PUT    /api/v1/projects/:id/files/code/rename  — renomeia o arquivo de código

GET    /api/v1/projects/:id/files/code/versions  — lista todas as versões salvas
POST   /api/v1/projects/:id/files/code/versions  — salva uma nova versão (Monaco)

POST   /api/v1/projects/:id/files/img          — faz upload de imagem
DELETE /api/v1/projects/:id/files/img/:name    — remove uma imagem

POST   /api/v1/projects/:id/files/docs         — cria um novo arquivo .md
PUT    /api/v1/projects/:id/files/docs/:name   — atualiza um .md existente (readme.md atualiza o card)
DELETE /api/v1/projects/:id/files/docs/:name   — remove um .md (readme.md retorna 403)
```

Todas as rotas exigem autenticação via Bearer JWT. Um usuário só pode acessar seus próprios projects.

---

---

## Templates

### O que é um template?

Um **template** é um projeto completo e configurável criado por um **especialista**. Ele empacota numa estrutura bem definida tudo que um **maker** precisa para gerar um projeto Go funcional sem escrever uma linha de código.

O maker abre a IDE, vê os devices do template como blocos visuais, conecta fios, configura valores no painel Inspect e clica em **Export → Go Code**. O servidor aplica os valores configurados e entrega um ZIP com o projeto pronto para compilar.

---

### Quem cria um template?

| Papel                 | O que faz                                                      |
|-----------------------|----------------------------------------------------------------|
| **Especialista**      | Escreve o código Go dos devices, monta o ZIP e faz upload      |
| **Servidor / Worker** | Analisa o ZIP, valida tudo e disponibiliza na IDE              |
| **Maker**             | Usa a IDE para configurar o template e baixar o projeto gerado |

---

### Estrutura do ZIP

O especialista entrega um arquivo `.zip` com esta estrutura obrigatória:

```
meu-template.zip
├── template.json          ← manifesto obrigatório (raiz do ZIP)
├── devices/
│   ├── ServerConfig.go    ← struct IDS — vira bloco visual na IDE
│   ├── DatabaseConfig.go
│   └── ColorPalette.go
└── output/                ← arquivos que serão entregues ao maker
    ├── go.mod             ← pode conter {{.ModuleName}}
    ├── main.go
    ├── config/
    │   └── app.yaml       ← pode conter {{.DBDriver}}, {{.StoreName}}
    └── public/
        ├── index.html     ← pode conter {{.PrimaryColor}}
        └── style.css
```

---

### O manifesto template.json

O `template.json` é o coração do template. Ele declara os metadados e o mapeamento entre as variáveis dos arquivos `output/` e os campos dos devices.

```json
{
  "name": "Online Store",
  "version": "1.0.0",
  "description": "Projeto completo de e-commerce em Go",
  "vars": {
    "StoreName":     "StoreConfig.Name",
    "PrimaryColor":  "ColorPalette.Primary",
    "DBDriver":      "DatabaseConfig.Driver",
    "ModuleName":    "StoreConfig.ModuleName"
  }
}
```

**Campos do manifesto:**

| Campo           | Obrigatório | Descrição                                 |
|-----------------|-------------|-------------------------------------------|
| `name`          | ✅           | Nome legível do template (aparece na IDE) |
| `version`       | ✅           | Versão semântica, ex: `"1.0.0"`           |
| `description`   | —           | Descrição curta exibida na IDE            |
| `minIDEVersion` | —           | Versão mínima da IDE exigida              |
| `vars`          | —           | Mapeamento `"NomeDaVar": "Device.Campo"`  |

**O mapeamento `vars`:**

Cada entrada conecta uma variável usada nos arquivos `output/` a um campo de um device:

```
"StoreName" → "StoreConfig.Name"
     │                │
     │                └─ campo "Name" do struct StoreConfig (em devices/)
     │
     └─ será substituído em {{.StoreName}} nos arquivos output/
```

Só campos marcados com a tag `prop` no struct são elegíveis. Campos recebidos por fio são dinâmicos e não podem ser usados como variáveis de template.

---

### A pasta devices/

Cada arquivo `.go` dentro de `devices/` segue o padrão **IDS** (IoTMaker Doc Standard) — o mesmo formato de qualquer black-box normal. O especialista escreve structs Go normais:

```go
// label:Store Configuration. icon:store.
type StoreConfig struct {
    // Nome da loja exibido no site.
    Name string `prop:"Store Name" default:"Minha Loja"`

    // Módulo Go do projeto (usado no go.mod).
    ModuleName string `prop:"Module Name" default:"github.com/example/store"`
}

// Run retorna a configuração atual.
// Outputs:
//   name (string) — nome da loja
func (s *StoreConfig) Run() (name string) {
    return s.Name
}
```

**Regras importantes:**

- Um único struct exportado por arquivo
- Ao menos um método (Init ou qualquer outro)
- Campos configuráveis estaticamente → tag `prop`
- Campos recebidos por fio → parâmetros do método (igual a qualquer black-box)
- Subdirectórios dentro de `devices/` não são suportados

Os devices aparecem na IDE com suporte completo a fios, exatamente como hardware black-boxes normais.

---

### A pasta output/

Contém os arquivos que serão entregues ao maker após a geração. O servidor substitui as variáveis `{{.NomeDaVar}}` pelo valor configurado pelo maker.

**Arquivos de texto** (HTML, CSS, YAML, TOML, JSON, SQL, Markdown, etc.) são processados como templates Go:

```html
<!-- public/index.html -->
<title>{{.StoreName}}</title>
<style>:root { --primary: {{.PrimaryColor}}; }</style>
```

```yaml
# config/app.yaml
store:
  name: "{{.StoreName}}"
database:
  driver: "{{.DBDriver}}"
```

**Arquivos binários** (imagens, fontes, PDFs) são copiados sem modificação.

**Arquivos `.go`** dentro de `output/` são **ignorados silenciosamente**. O código Go do projeto gerado sempre vem do pipeline de codegen por fios — nunca diretamente da pasta `output/`.

> **Convenção de nomes:** variáveis de template devem começar com letra maiúscula (`{{.StoreName}}`, não `{{.storeName}}`). Isso é obrigatório pelo Go e evita conflito com palavras-chave do `text/template`.

---

### Ciclo de vida do template

```
ESPECIALISTA                    SERVIDOR                      MAKER
────────────                    ────────                      ─────
Monta o ZIP            POST /api/v1/templates
  devices/ + output/  ──────────────────────►  Salva ZIP no disco
  template.json                                Status: pending
                                               Enfileira tarefa
                                                     │
                                                     ▼
                                              Worker analisa ZIP
                                              ├─ Valida template.json
                                              ├─ Parseia devices/*.go
                                              ├─ Mapeia output/ files
                                              └─ Status: ready (ou error)

                                                               Abre a IDE
                                GET /api/v1/templates         ◄──────────
                              ◄─────────────────────────────
                                Lista templates disponíveis
                                                               Coloca devices
                                                               no canvas, liga
                                                               fios, configura
                                                               props

                                POST /api/v1/templates/:id/generate
                              ◄────────────────────────────────────
                                { "config": { "StoreName": "Minha Loja", ... } }
                                     │
                                     ▼
                                Aplica text/template
                                em cada arquivo output/
                                Copia binários
                                Monta ZIP de saída
                                ─────────────────────────────►  Download do ZIP
                                                                 (projeto pronto)
```

---

### Usando um template na IDE

1. O maker abre a IDE e vê o menu **Templates** (aparece apenas quando há templates disponíveis)
2. Seleciona o template e coloca os devices no canvas (Init + métodos)
3. **Configura via fio** — conecta um `INT 8082` na porta `port` do Init
4. **Configura via Inspect** — preenche `Message`, `ModuleName` e outros campos no painel lateral
5. Clica em **Export → Go Code**
6. A IDE resolve os valores (fio > prop > default do template) e envia para o servidor
7. O servidor gera o ZIP e o browser faz o download automaticamente

**Prioridade de resolução de valores:**

```
1. Fio conectado ao conector de entrada   →  valor do fio (ex: 8082)
2. Valor no painel Inspect (prop)         →  valor digitado (ex: "8081")
3. Default do manifesto do template       →  valor padrão do especialista
```

---

### Visibilidade do template

| Visibilidade       | Quem pode usar                           |
|--------------------|------------------------------------------|
| `private` (padrão) | Apenas o especialista que fez o upload   |
| `public`           | Qualquer maker autenticado na plataforma |

Um template só pode ser tornado público quando seu status é `ready`. Templates com status `pending` ou `error` não podem ser publicados.

---

### API de templates

```
POST   /api/v1/templates                  — faz upload de um ZIP (retorna status "pending")
GET    /api/v1/templates                  — lista templates visíveis para o usuário logado
GET    /api/v1/templates/:id              — retorna o template completo (com devices e vars)
PUT    /api/v1/templates/:id/visibility   — muda visibilidade: "public" ou "private" (dono)
DELETE /api/v1/templates/:id             — exclui o template e o ZIP no disco (dono)
POST   /api/v1/templates/:id/generate    — gera e retorna o ZIP configurado (maker)
```

**Limites de segurança:**

| Rota     | Limite                           |
|----------|----------------------------------|
| Upload   | 5 requisições por minuto por IP  |
| Upload   | Tamanho máximo de 50 MB por ZIP  |
| Generate | 10 requisições por minuto por IP |

O upload é **assíncrono**: o servidor responde `202 Accepted` imediatamente e o worker processa o ZIP em segundo plano. O cliente deve consultar `GET /api/v1/templates/:id` periodicamente até que `status != "pending"`.

---

---

## Diferença entre project e template

| Aspecto                | Project                                         | Template                                   |
|------------------------|-------------------------------------------------|--------------------------------------------|
| **Quem cria**          | Qualquer usuário                                | Especialista                               |
| **O que contém**       | Código Go de um device                          | ZIP com devices + assets + manifesto       |
| **Como é usado**       | Publicado no marketplace, usado como componente | Usado na IDE para gerar projetos completos |
| **Saída para o maker** | Código fonte do device                          | ZIP com projeto Go configurado e pronto    |
| **Configuração**       | Direto no Monaco editor                         | Via fios e painel Inspect na IDE           |
| **Publicação**         | Feed + Busca do marketplace                     | Visibilidade pública/privada               |
| **Versionamento**      | Versões numeradas do código                     | Re-upload de um novo ZIP                   |
| **Geração de código**  | Pipeline codegen por fios                       | text/template sobre arquivos output/       |
