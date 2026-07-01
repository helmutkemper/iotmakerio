# graph — SceneJSON → Grafo de Computação

## O que este pacote faz

Converte o JSON exportado pela IDE em uma estrutura de dados de grafo que o
restante do pipeline pode usar sem conhecer o formato JSON.

O grafo tem três conceitos fundamentais:

- **Node** — um device no canvas (constante, operação aritmética, loop, sensor)
- **Edge** — um fio conectando a saída de um node à entrada de outro
- **Scope** — um agrupamento de nodes por containment (global ou dentro de um loop)

---

## Por que não usar o SceneJSON diretamente?

O SceneJSON é rico em informação visual (posições, tamanhos, bounding boxes,
camera) que o codegen não precisa. Converter para um grafo tem dois benefícios:

**1. Separação de responsabilidades:**
O emitter IR e o backend Go trabalham apenas com `Node`, `Edge` e `Scope` —
tipos simples com semântica clara. Se o formato do SceneJSON mudar, apenas o
builder precisa ser atualizado.

**2. Zero dependências de WASM:**
Os tipos de entrada (`SceneInput`, `DeviceInput`, etc.) são definidos localmente
neste pacote. O servidor codegen pode ser compilado e testado sem nenhuma
referência ao código WASM, ao sprite engine, ou ao canvas.

---

## Tipos do grafo

### Node

```go
type Node struct {
    ID             string                 // ex: "constInt_1", "apds9960_1_init"
    Type           string                 // ex: "StatementConstInt", "BlackBoxInit:APDS9960"
    Label          string                 // rótulo opcional do maker
    Properties     map[string]interface{} // dados brutos do JSON (value, instanceId, props...)
    ScopeID        string                 // "" = escopo global, senão = ID do loop container
    Inputs         []Port                 // portas de entrada com suas conexões
    Outputs        []Port                 // portas de saída com suas conexões
    ExecutionOrder int                    // 0 = sem ordem, >0 = ordem explícita (IDS tag)
}
```

### Port

```go
type Port struct {
    Name      string    // ex: "inputX", "output", "stop", "bus"
    DataType  string    // ex: "int", "bool", "*machine.I2C"
    IsOutput  bool
    WireIDs   []string  // IDs dos fios conectados (para debug)
    Connected []PortRef // referências aos devices/portas do outro lado
}
```

### Edge

```go
type Edge struct {
    ID       string  // ID do fio
    From     PortRef // {DeviceID, PortName} da saída
    To       PortRef // {DeviceID, PortName} da entrada
    DataType string
}
```

### Scope

```go
type Scope struct {
    ID       string   // "" para global, ID do loop para escopos aninhados
    ParentID string   // ID do escopo pai
    NodeIDs  []string // IDs dos nodes diretamente neste escopo (não aninhados)
    StopPort *PortRef // para loops: qual device+porta está conectado ao "stop"
}
```

**Por que `StopPort` fica no Scope e não no Node?**

O loop (`StatementLoop`) é ao mesmo tempo um node (tem um ID no grafo) e um
escopo (contém outros nodes). Guardar `StopPort` no `Scope` evita que o emitter
IR precise saber que o ID do escopo é também um node — ele consulta apenas o
scope e lê diretamente de onde vem a condição de parada.

---

## O algoritmo de Build — 4 passes

O builder faz 4 passagens sobre o JSON em sequência. A ordem importa: cada
passe depende do que o passe anterior produziu.

### Pass 1 — Criação de nós e escopos

Varre todos os devices e cria:
- Um `Node` para cada device
- Um `Scope` para cada device com `isContainer: true` (loops)
- Popula as listas `Inputs` e `Outputs` de cada node a partir dos `connectors`
- Extrai `ExecutionOrder` de `properties.executionOrder`

O escopo global (`""`) é criado antes do passe 1 e sempre existe.

**Por que extrair ExecutionOrder aqui?**

O JSON decodifica números como `float64` em Go (comportamento padrão de
`json.Unmarshal` em `interface{}`). O passe 1 converte para `int` na hora certa,
evitando conversões espalhadas pelo código:

```go
if v, ok := dev.Properties["executionOrder"]; ok {
    switch n := v.(type) {
    case float64:
        node.ExecutionOrder = int(n)
    case int:
        node.ExecutionOrder = n
    }
}
```

### Pass 2 — Atribuição de escopos por containment

Usa `containment.parent` e `containment.status` para decidir onde cada node vive:

| `isContainer` | `parent` | `status` | Resultado |
|---|---|---|---|
| false | `""` | `"free"` | Escopo global |
| false | `"stmLoop_1"` | `"contained"` | Dentro do loop `stmLoop_1` |
| true | `""` | `"container"` | Container no escopo global |
| true | `"stmLoop_1"` | `"contained"` | Container aninhado dentro de outro loop |
| qualquer | qualquer | `"error"` | Node parcialmente fora do container — warning |

**Containers são membros do escopo pai, não do próprio escopo:**
Um loop em `stmLoop_1` que está no escopo global tem `ScopeID = ""` (global)
e aparece em `g.Scopes[""].NodeIDs`. Isso é correto: o loop *pertence* ao
escopo global, mesmo que seus filhos pertençam ao escopo `stmLoop_1`.

### Pass 3 — Criação de arestas (edges) a partir dos wires

Simples: cria um `Edge` para cada wire no JSON. As informações são redundantes
com os `connections` nos connectors (já processados no Pass 1), mas os wires
são a fonte autoritativa para a lista de arestas do grafo.

### Pass 4 — Resolução de StopPort para loops

Para cada scope que não é global, encontra o loop node correspondente e
varre seus inputs procurando a porta `"stop"`. Quando encontrada e conectada,
resolve a referência e salva em `scope.StopPort`.

**Por que um passe separado para StopPort?**

No Pass 2, nem todos os scopes foram completamente montados ainda. O Pass 4
garante que todos os nodes e arestas já existem antes de tentar resolver a
referência de stop.

---

## Métodos de consulta no grafo

```go
// GetInputSources retorna de onde vem o valor de uma porta de entrada
g.GetInputSources("add_1", "inputX")
// → [{DeviceID: "constInt_1", PortName: "output"}]

// GetOutputTargets retorna onde vai o valor de uma porta de saída
g.GetOutputTargets("constInt_1", "output")
// → [{DeviceID: "add_1", PortName: "inputX"}]

// ScopeOf retorna o escopo de um node
g.ScopeOf("constInt_1")
// → "" (global)
g.ScopeOf("add_1_dentro_do_loop")
// → "stmLoop_1"
```

Estes métodos são usados intensamente pelo emitter IR para construir
dependências entre nodes.

---

## Exemplo visual: como a cena vira um grafo

**Cena:**
```
[ConstInt 10] ──→ [Add] ──→ [Gauge "total"]
[ConstInt 20] ──→ [Add]
```

**Grafo resultante:**
```
Nodes:
  constInt_1  type=StatementConstInt  props={value:10}  scope=""
              outputs=[{name:"output", connected:[{add_1, inputX}]}]

  constInt_3  type=StatementConstInt  props={value:20}  scope=""
              outputs=[{name:"output", connected:[{add_1, inputY}]}]

  add_1       type=StatementAdd  scope=""
              inputs =[{name:"inputX", connected:[{constInt_1, output}]},
                       {name:"inputY", connected:[{constInt_3, output}]}]
              outputs=[{name:"output", connected:[{gauge_1, current}]}]

  gauge_1     type=StatementGauge  label="total"  scope=""
              inputs=[{name:"current", connected:[{add_1, output}]}]

Edges:
  w1: constInt_1.output → add_1.inputX
  w2: constInt_3.output → add_1.inputY
  w3: add_1.output → gauge_1.current

Scopes:
  "": {NodeIDs: [constInt_1, constInt_3, add_1, gauge_1]}
```

---

## Situações especiais

### Fio de saída de scope (scope crossing)

Quando `add_1` está dentro de um loop e `gauge_1` está fora:

```
Scope global:
  stmLoop_1 (container)
    └── add_1
  gauge_1

Wire: add_1.output → gauge_1.current
```

O builder cria a aresta normalmente. Quem detecta o cruzamento de escopo é o
emitter IR (`analyzeScopeCrossings`), que marca `add_1` como "promoted to VAR"
para que seja declarado antes do loop.

### Fio para porta "stop"

O wire que vai para a porta `stop` do loop é criado como aresta normal no Pass 3.
O Pass 4 detecta essa conexão específica e salva em `scope.StopPort`.

O emitter IR trata `stop` de forma especial: não cria dependência de ordenação
para o node que alimenta o stop (a comparação que determina a condição de parada
já está na ordem correta por dependência de dados), mas usa `scope.StopPort`
para emitir o `BREAK_IF` correto.
