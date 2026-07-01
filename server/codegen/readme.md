# server/codegen — Pipeline de Geração de Código

> Este documento é o ponto de entrada para qualquer desenvolvedor que precise
> entender, modificar ou estender o sistema de geração de código da IDE.
> Leia-o antes de qualquer outro arquivo da pasta.

---

## O que este pacote faz

O pacote `codegen` recebe uma **cena exportada pela IDE** (um JSON descrevendo
blocos visuais e fios) e produz **código-fonte Go** compilável.

O fluxo completo:

```
Maker conecta blocos na IDE
        ↓
IDE exporta SceneJSON
        ↓
POST /api/v1/codegen/go    (handler chama codegen.Generate)
        ↓
graph.Build   — JSON → grafo de computação
        ↓
validate      — verificações de sanidade (inputs conectados, loops com stop)
        ↓
ir.Emit       — grafo → lista linear de instruções (IR)
        ↓
golang.Emit   — IR → código-fonte Go
        ↓
string com package main + imports + structs + func main()
```

---

## Por que existe uma Representação Intermediária (IR)?

Esta é a decisão de design mais importante do pipeline e merece explicação.

Uma abordagem ingênua seria converter o SceneJSON diretamente em texto Go.
Isso funciona para casos simples mas cria problemas sérios:

**Problema 1 — Acoplamento de formato:**
Se o SceneJSON mudar (novos campos, nova estrutura de containment), toda a
lógica de geração de código precisa mudar junto.

**Problema 2 — Lógica de ordenação duplicada:**
A ordenação topológica, a detecção de cruzamento de escopos e o hoisting de
variáveis são algoritmos não-triviais. Se fossem implementados diretamente no
backend Go, teriam que ser reimplementados para cada linguagem-alvo (C, Python,
MicroPython, etc.).

**Problema 3 — Testabilidade:**
É muito mais fácil testar "o IR gerado para esta cena contém LOOP_BEGIN antes
de ADD" do que testar "o Go gerado contém `for {` antes de `a + b`". O IR é uma
representação estável e de alto nível que pode ser inspecionada diretamente.

**A solução — IR como contrato:**
O IR é uma lista linear de instruções com opcodes simples (`CONST`, `ADD`,
`LOOP_BEGIN`, `BB_INIT`, etc.). O emitter `ir/emit.go` implementa **toda** a
lógica de análise (ordenação topológica, cruzamento de escopos, hoisting de
black-boxes). O backend `backend/golang/emit.go` apenas traduz cada instrução
para texto Go, sem nenhuma análise.

Resultado: adicionar suporte a uma nova linguagem significa escrever apenas
um novo backend. Toda a lógica complexa já está no IR emitter.

---

## Estrutura de arquivos

```
server/codegen/
│
├── codeGen.go              Ponto de entrada: Request → Generate(ctx, req) → Response
├── codeGen_test.go         Testes de integração: cena linear, loop com cruzamento de escopo
├── blackBox_test.go        Testes do parser e pipeline completo com black-boxes
│
├── blackbox/               Parser de código Go do especialista → BlackBoxDef
│   ├── types.go            Tipos: BlackBoxDef, FuncDef, PortDef, PropDef, ManualPage
│   └── parser.go           Parse(): AST walk + extração de manual pages
│
├── graph/                  SceneJSON → grafo de computação
│   ├── types.go            Node, Port, Edge, Scope, Graph
│   └── builder.go          Build(): 4 passes sobre o JSON
│
├── ir/                     Grafo → programa IR linear
│   ├── types.go            Op constants, Instruction, Program
│   └── emit.go             Emit(): topo sort, scope crossing, BB hoisting
│
└── backend/
    └── golang/
        └── emit.go         IR → código-fonte Go
```

---

## Responsabilidade de cada camada

| Camada | Entrada | Saída | Responsabilidade |
|---|---|---|---|
| `codeGen.go` | `Request` (JSON + BlackBoxDefs) | `Response` (código + IR + erros) | Orquestração do pipeline |
| `graph/` | `SceneInput` (structs Go do JSON) | `*Graph` | Estrutura de dados: nós, arestas, escopos |
| `ir/` | `*Graph` | `*Program` | Ordenação, análise de escopos, hoisting |
| `backend/golang/` | `*Program` | `string` | Tradução IR → texto Go |
| `blackbox/` | `[]byte` (código Go do especialista) | `*BlackBoxDef` | Extração de metadados por AST |

---

## O JSON de entrada (SceneJSON)

Este é o contrato entre a IDE WASM e o servidor. A IDE serializa o estado do
canvas e envia como `Request.Scene`. O handler nunca chama a IDE — o JSON é
suficiente para reconstruir toda a computação.

### Estrutura completa com anotações

```json
{
  "version": "1.0",
  "metadata": {
    "density": 1,           // DPI da tela (não usado pelo codegen)
    "canvasWidth": 1200,    // dimensões do canvas (não usado pelo codegen)
    "canvasHeight": 800,
    "camera": {
      "offsetX": 0,         // posição da câmera (não usado pelo codegen)
      "offsetY": 0,
      "zoom": 1.0
    }
  },
  "devices": [ ... ],       // ← os nós do grafo (veja abaixo)
  "wires":   [ ... ]        // ← as arestas do grafo (veja abaixo)
}
```

> **Nota para futuros desenvolvedores:** os campos de `metadata` e as posições
> (`position`, `size`, `outerBBox`) não são utilizados pelo codegen — eles
> existem para a IDE poder restaurar o estado visual do canvas. O codegen usa
> apenas `type`, `properties`, `connectors` e `containment`.

### Estrutura de um Device (nó do grafo)

```json
{
  "id":    "constInt_1",          // identificador único, gerado pela IDE
  "type":  "StatementConstInt",   // tipo do device — veja tabela abaixo
  "label": "minha constante",     // rótulo opcional definido pelo maker

  "properties": {                 // dados específicos do tipo
    "value": 42                   // para ConstInt: o valor numérico
  },

  "position": { "x": 100, "y": 200 },   // posição visual (ignorada pelo codegen)
  "size":     { "width": 80, "height": 50 },
  "outerBBox": { "x": 100, "y": 200, "width": 80, "height": 50 },
  "innerBBox": null,              // só presente em containers (loops)

  "overlapPolicy": {
    "allowAbove":   false,        // este device pode estar acima de um container?
    "allowBelow":   true,         // abaixo?
    "allowPartial": false         // parcialmente fora do container?
  },

  "connectors": [                 // portas de entrada e saída
    {
      "port":               "output",    // nome da porta
      "dataType":           "int",       // tipo de dado do fio
      "isOutput":           true,        // false = input, true = output
      "acceptNotConnected": true,        // true = conexão opcional, false = obrigatória
      "position": { "x": 172, "y": 225 }, // posição visual do conector (ignorada pelo codegen)
      "connections": [                   // lista de conexões desta porta
        {
          "wireId":       "wire_1",       // ID do fio que conecta
          "targetDevice": "add_1",        // device do outro lado
          "targetPort":   "inputA"        // porta do outro lado
        }
      ]
    }
  ],

  "containment": {
    "isContainer": false,         // true se este device é um container (loop)
    "children":    [],            // IDs dos devices diretamente dentro deste container
    "overlapping": [],            // IDs dos devices que estão parcialmente dentro
    "parent":      "",            // ID do container pai ("" = escopo global)
    "status":      "free"         // "free" | "contained" | "container" | "error"
  }
}
```

### Tipos de device e seus campos `properties`

| `type` | `properties` usadas pelo codegen | Notas |
|---|---|---|
| `StatementConstInt` | `value: number` | Constante inteira |
| `StatementBool` | `value: bool` | Constante booleana |
| `StatementAdd` | — | inputX, inputY, output |
| `StatementSub` | — | inputX, inputY, output |
| `StatementMul` | — | inputX, inputY, output |
| `StatementDiv` | — | inputX, inputY, output |
| `StatementEqualTo` | — | inputX, inputY, output (bool) |
| `StatementNotEqualTo` | — | inputX, inputY, output (bool) |
| `StatementLessThan` | — | inputX, inputY, output (bool) |
| `StatementLessThanOrEqualTo` | — | inputX, inputY, output (bool) |
| `StatementGreaterThan` | — | inputX, inputY, output (bool) |
| `StatementGreaterThanOrEqualTo` | — | inputX, inputY, output (bool) |
| `StatementLoop` | — | Container. Porta especial: `stop` (bool) |
| `StatementGauge` | — | Usa `label` como nome do canal de saída |
| `BlackBoxInit:StructName` | `instanceId`, `executionOrder`, `props: {}` | Black-box |
| `BlackBoxRun:StructName` | `instanceId`, `executionOrder` | Black-box |

**Black-box properties em detalhe:**

```json
{
  "id":   "apds9960_1_init",
  "type": "BlackBoxInit:APDS9960",
  "properties": {
    "instanceId":     "apds9960_1",  // ← compartilhado entre Init e Run da mesma instância
    "executionOrder": 10,             // ← ordem quando não há fio conectando (IDS tag)
    "props": {                        // ← valores das propriedades configuradas pelo maker
      "gain":  "0",
      "atime": "255"
    }
  }
}
```

O `instanceId` é o campo crítico para black-boxes: Init e Run do **mesmo**
componente físico compartilham o mesmo `instanceId`. O codegen usa isso para
saber que `apds9960_1_init` e `apds9960_1_run` referem-se à mesma variável
`var apds99601 APDS9960` no código gerado.

### Estrutura de um Wire (aresta do grafo)

```json
{
  "id":       "wire_1",
  "from": { "device": "constInt_1", "port": "output" },
  "to":   { "device": "add_1",      "port": "inputA" },
  "dataType": "int"
}
```

Os fios são redundantes com as `connections` nos connectors — ambos descrevem
a mesma aresta. O builder usa os wires para criar as arestas do grafo e usa as
connections nos connectors para popular as listas `Port.Connected` nos nós.

---

## Exemplos de SceneJSON completos

Os testes em `codeGen_test.go` contêm dois exemplos completos e comentados
que servem como referência canônica:

- `sceneLinear` — dois ConstInt → Add → Gauge (sem loop)
- `sceneLoop` — Loop com ConstInt + Add + Compare → stop, Gauge fora do loop

O `blackBox_test.go` contém `TestBlackBoxPipeline` que usa I2CBus + APDS9960
em um setup realista com Init global e Run dentro de loop.

---

## Fluxo de dados para Black-Boxes

Black-boxes têm um fluxo de dados ligeiramente diferente porque precisam de
metadados extras (código-fonte do struct, assinaturas dos métodos) que não
estão no SceneJSON:

```
Handler HTTP (server/handler/codegen/submit.go) recebe Request.Scene
        ↓
Handler carrega BlackBoxDefs do banco de dados (um parse por struct)
        ↓
Handler serializa BlackBoxDefs no payload da task Asynq (tasks.CodegenPayload)
        ↓
Handler enfileira codegen:run e devolve 202 + stream_url
        ↓
Worker (server/cmd/worker/main.go) consome a task da fila "codegen"
        ↓
Worker deserializa o payload e popula Request.BlackBoxDefs (map[string]*BlackBoxDef)
        ↓
Generate(ctx, req) — pipeline com 4 checkpoints de cancelamento entre as 5 etapas
        ↓
ir.Emit não usa BlackBoxDefs — apenas anota Meta nas instruções BB_
        ↓
program.BlackBoxDefs = req.BlackBoxDefs  ← anexado ao programa
        ↓
golang.Emit lê BlackBoxDefs para emitir StructCode, MethodsCode, Imports
        ↓
golang.Emit usa BlackBoxDef.Props para traduzir scene JSON keys → Go FieldName
(server/codegen/backend/golang/emit.go: resolveBBFieldName)
        ↓
Worker publica resultado em codegen:job:{id}:result (TTL 10min)
        ↓
SSE stream handler (server/handler/codegen/stream.go) entrega ao cliente WASM
```

O IR não precisa conhecer o conteúdo do struct — ele só sabe que existe uma
instância chamada `apds9960_1` do tipo `APDS9960`. O backend Go é que lê o
código-fonte do struct e o inclui na saída.

---

## Erros vs. Warnings

| Tipo | Significado | Pipeline continua? |
|---|---|---|
| `Response.Errors` | Problema que impede geração correta | Não — retorna imediatamente |
| `Response.Warnings` | Situação suspeita mas recuperável | Sim — código é gerado mesmo assim |

Exemplos de erros: loop sem condição de stop, input obrigatório não conectado,
black-box não encontrado no registro, ciclo detectado no grafo.

Exemplos de warnings: porta de saída não conectada a nada, tipo desconhecido.

---

## Adicionando suporte a uma nova linguagem

1. Crie `backend/novaLinguagem/emit.go` no mesmo padrão de `backend/golang/emit.go`.
2. A função principal deve ser `func Emit(prog *ir.Program) string`.
3. Itere sobre `prog.Instructions` e trate cada `Op` conforme a sintaxe da linguagem.
4. Adicione o case em `codeGen.go`:
	 ```go
	 case "nova_linguagem":
			 resp.Code = novaLinguagem.Emit(program)
	 ```

Todo o trabalho pesado (ordenação, escopos, hoisting) já está feito no IR.
O novo backend só precisa traduzir texto.

---

## Rodando os testes

```bash
cd server
go test ./codegen/...        # todos os testes
go test ./codegen/ -v        # com output detalhado (mostra IR e Go gerados)
go test ./codegen/ -run TestBlackBoxPipeline -v   # apenas pipeline de black-box
```

Os testes são de integração — cada um roda o pipeline completo do JSON ao código
gerado e verifica o IR e o Go com asserções de substring.
