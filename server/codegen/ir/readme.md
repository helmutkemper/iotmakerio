# ir — Representação Intermediária (IR)

## O que este pacote faz

Recebe um `*graph.Graph` e produz um `*Program` — uma lista linear e ordenada
de instruções simples que um backend pode traduzir para qualquer linguagem-alvo.

Este é o pacote mais complexo do pipeline. Ele resolve todos os problemas de
análise de fluxo de dados que seriam impossíveis de resolver diretamente no
backend de linguagem.

---

## Conceito fundamental: o IR como lista linear

O grafo de computação é uma estrutura com dependências parciais e escopos
aninhados. O código gerado é uma sequência plana de instruções.

A transformação grafo → lista linear é o trabalho do emitter.

Analogia: é como compilar um diagrama de fluxo em assembly. O diagrama tem
caixas e setas; o assembly tem instruções numeradas em sequência. Alguém precisa
decidir em que ordem as caixas viram instruções e como os loops se tornam saltos.
O emitter IR faz isso para a IDE.

---

## Opcodes — referência completa

### Constantes e variáveis

```
CONST %dest type value
```
Declara e inicializa uma variável local com `:=`.
Exemplo: `CONST %constInt_1 int 10` → `constInt1 := int64(10)`

```
VAR %dest type zeroValue
```
Declara uma variável sem inicialização curta (usa `var`).
Usado para registros **promovidos** (variáveis que precisam sobreviver ao
cruzamento de escopo — declaradas antes do loop, usadas dentro e fora).
Exemplo: `VAR %add_1 int 0` → `var add1 int64`

```
ASSIGN %dest type value
```
Atribuição simples (`=`) para um `VAR` já declarado.
Usado quando um valor promovido é recalculado dentro do loop.

### Aritmética

```
ADD %dest type %a %b     →  dest := a + b
SUB %dest type %a %b     →  dest := a - b
MUL %dest type %a %b     →  dest := a * b
DIV %dest type %a %b     →  dest := a / b
```

### Comparação

```
CMP_EQ %dest bool %a %b  →  dest := a == b
CMP_NE %dest bool %a %b  →  dest := a != b
CMP_LT %dest bool %a %b  →  dest := a < b
CMP_GT %dest bool %a %b  →  dest := a > b
CMP_LE %dest bool %a %b  →  dest := a <= b
CMP_GE %dest bool %a %b  →  dest := a >= b
```

### Controle de fluxo

```
LOOP_BEGIN %loopId        →  for {
BREAK_IF %condition       →  if condition { break }
LOOP_END %loopId          →  }
```

`LOOP_BEGIN` e `LOOP_END` usam o mesmo `%loopId` para formar um par. O emitter
os gera como marcadores — o backend os traduz para `for {` e `}`.

### Saída

```
OUTPUT %source "channelName"   →  fmt.Println("channelName", source)
RETURN %source                 →  _ = source  // valor de retorno
```

### Black-box (BB_*)

```
BB_DECL %instanceId  {struct=StructName}
```
Declara `var instanceId StructName`. Sempre emitido **antes** das instruções
que usam a instância (hoisted). Ver seção "Hoisting de BB_DECL" abaixo.

```
BB_PROP %instanceId  fieldName value  {struct=StructName}
```
Atribui um valor de propriedade: `instanceId.fieldName = value`.
Emitido logo antes de `BB_INIT`.

```
BB_INIT %instanceId  [%input1, %input2, ...]  {struct=StructName, nodeId=...}
```
Chama `Init()`: `out1, out2 := instanceId.Init(in1, in2)`.

```
BB_RUN  %instanceId  [%input1, %input2, ...]  {struct=StructName, nodeId=...}
```
Chama `Run()`: `out1, out2, out3 := instanceId.Run(in1, in2)`.

---

## O formato dos registros (%registers)

Todo valor no IR é referenciado por um "register" prefixado com `%`:

- `%constInt_1` — valor de saída do device `constInt_1`
- `%add_1` — valor de saída do device `add_1`
- `%i2cBus_1:bus` — porta `bus` da instância `i2cBus_1` (forma composta)

A forma composta `%instanceId:portName` é usada exclusivamente para saídas de
black-boxes, porque um device black-box pode ter múltiplas saídas com nomes
distintos (`bus`, `err`, `clear`, `red`, ...).

O backend Go converte `%i2cBus_1:bus` → `i2cBus1_bus` (identificador Go válido).

---

## Os 3 problemas que o emitter resolve

### Problema 1 — Ordenação topológica

Os nodes no grafo não têm ordem definida. O emitter precisa garantir que um
node só é emitido depois que todos os seus inputs já foram emitidos.

**Solução: algoritmo de Kahn (BFS topológico)**

Para cada escopo (global e cada loop), o emitter:
1. Calcula o in-degree de cada node (quantos nodes precisam rodar antes dele)
2. Coloca na fila os nodes com in-degree = 0 (sem dependências)
3. Remove um node da fila, o emite, decrementa o in-degree dos seus dependentes
4. Quando um dependente chega a in-degree = 0, entra na fila
5. Repete até a fila estar vazia

Se ao final sobrou algum node não emitido, há um ciclo no grafo — erro.

**Desempate por `executionOrder`:**

Quando múltiplos nodes têm in-degree = 0 simultaneamente (não há fio entre eles),
a ordem seria não-determinística. O emitter usa `executionOrder` como chave de
ordenação primária: nodes com `executionOrder` menor entram primeiro na fila.
Nodes sem `executionOrder` (valor 0) usam `math.MaxInt32` como sentinela,
ficando após todos os nodes ordenados. Entre nodes com o mesmo `executionOrder`,
o desempate final é por ID lexicográfico (determinístico).

```go
nodeOrder := func(id string) (group int, lexID string) {
    if node.ExecutionOrder > 0 {
        return node.ExecutionOrder, id
    }
    return 1<<31 - 1, id  // MaxInt32 — sempre depois dos ordenados
}
```

### Problema 2 — Cruzamento de escopos

Um fio pode conectar um node **dentro** de um loop a um node **fora** do loop.
Isso cria um problema: a variável precisa existir tanto dentro do loop (onde
é escrita) quanto fora (onde é lida).

```
[ConstInt 10] ─→ [Add]  ← dentro do loop
                  ↓
              [Gauge]  ← fora do loop
```

Em Go, uma variável declarada com `:=` dentro de um `for {}` não é visível
fora. A solução é declarar com `var` antes do loop.

**Detecção:**

`analyzeScopeCrossings` varre todas as arestas e verifica se `fromScope != toScope`.
Se o scope de destino é **ancestral** do scope de origem (o fio "sobe" de escopo),
o device de origem é marcado como "promoted".

```go
if e.isAncestor(toScope, fromScope) {
    e.promoted[edge.From.DeviceID] = true
}
```

**Emissão:**

No início do escopo global, o emitter emite `VAR` para todos os registros
promovidos. Depois, dentro do loop, usa `ASSIGN` em vez de `CONST` para esses
registros (já que estão declarados com `var`, não com `:=`).

### Problema 3 — Hoisting de BB_DECL

O problema: `var sensor1 APDS9960` precisa aparecer antes de qualquer uso de
`sensor1`, mas a ordenação topológica pode colocar `BB_INIT` e `BB_RUN` em
ordens diferentes dependendo das conexões.

**A regra:**

Se **qualquer** método da instância (Init ou Run) está no escopo global,
o `var` deve ser declarado no escopo global — antes do loop.
Se **todos** os métodos estão dentro de um loop, o `var` é declarado no topo
desse loop.

**Implementação:**

`buildInstanceScopeOwners` faz uma pré-passagem pelo grafo antes de emitir
qualquer instrução. Para cada instância de black-box, coleta todos os scopes
em que seus nodes aparecem. Se o escopo global (`""`) está no conjunto,
a instância é "owned" pelo escopo global:

```go
if scopes[""] {
    instanceScopeOwner[instanceId] = ""  // global
} else {
    // todos dentro de loops — owned pelo primeiro loop encontrado
    instanceScopeOwner[instanceId] = primeiroScopeDoConjunto
}
```

`emitBBDeclsForScope(scopeID, sorted)` é chamado **antes** do loop principal
de emissão em `emitScope`. Ele emite `BB_DECL` para todas as instâncias cujo
owner é `scopeID`, na ordem em que aparecem em `sorted` (determinístico).

---

## Arestas implícitas Init → Loop

Este é um caso especial que não tem representação no grafo mas precisa ser
resolvido na ordenação.

**Situação:** o maker colocou um `BlackBoxInit` no escopo global e um
`BlackBoxRun` dentro de um loop. Não há fio entre o Init e o loop — o Init
produz apenas um `err` (opcional) e nenhuma saída que o Run precise.

**Problema:** sem um fio, a ordenação topológica não sabe que o Init deve rodar
antes do loop. O loop poderia ser emitido primeiro.

**Solução:** durante `topoSort`, para cada par (BlackBoxInit no scope, Loop no
scope), se o loop contém um BlackBoxRun da mesma instância, é criada uma aresta
implícita Init → Loop:

```go
// Se stmLoop_1 contém apds9960_1_run, adiciona dependência:
// apds9960_1_init → stmLoop_1
inDegree[loopID]++
dependents[initID] = append(dependents[initID], loopID)
```

Isso garante que o Init sempre roda antes do loop, mesmo sem fio explícito.

---

## Sequência de emissão de um escopo

```
emitScope(scopeID):
  1. topoSort(scope.NodeIDs)         → sorted []string
  2. Se escopo global:
       emitPromotedVars()            → VAR para registros que cruzam escopo
       emitBBDeclsForScope("", ...)  → BB_DECL para instâncias globais
  3. Se escopo de loop:
       LOOP_BEGIN %loopId
       emitBBDeclsForScope(loopId, ...)  → BB_DECL para instâncias locais ao loop
  4. Para cada node em sorted:
       emitNode(nodeID)              → instruções específicas do tipo
  5. Se escopo de loop:
       BREAK_IF %stopCondition
       LOOP_END %loopId
```

A recursão acontece em `emitNode`: quando encontra um `StatementLoop`, chama
`emitScope(node.ID)`. Isso embute as instruções do loop no lugar certo na
sequência linear.

---

## Exemplo completo: IR para Add dentro de loop

**Cena:** ConstInt(10) + ConstInt(20) dentro de loop, comparação, Gauge fora.

**IR gerado:**
```
VAR %add_1 int 0             ← add_1 cruza escopo, precisa de var antes do loop
LOOP_BEGIN %stmLoop_1
  CONST %constInt_1 int 10   ← dentro do loop
  CONST %constInt_3 int 20
  ASSIGN %add_1 int 30       ← ASSIGN (não CONST) porque add_1 já foi declarado
  CMP_GT %compare_1 bool %add_1 %constInt_5
  BREAK_IF %compare_1
LOOP_END %stmLoop_1
OUTPUT %gauge_1 %add_1 "total"  ← fora do loop, add_1 ainda está acessível
```

**Go gerado:**
```go
func main() {
    var add1 int64
    for {
        constInt1 := int64(10)
        constInt3 := int64(20)
        add1 = constInt1 + constInt3
        compare1 := add1 > constInt5
        if compare1 { break }
    }
    fmt.Println("total", add1)
}
```

---

## Estrutura interna do emitter

```go
type emitter struct {
    graph   *graph.Graph
    program *Program

    promoted           map[string]bool   // nodes marcados para promoção a VAR
    emitted            map[string]bool   // nodes já emitidos (evita duplicatas)
    bbDeclared         map[string]bool   // instâncias BB já com BB_DECL emitido
    instanceScopeOwner map[string]string // instanceId → scopeID dono do var
}
```

Os quatro maps de estado são necessários porque o emitter é recursivo
(loops chamam `emitScope` recursivamente) e precisa de memória global
para não emitir o mesmo node ou declaração duas vezes.

---

## Testabilidade

O IR tem representação textual direta via `Program.String()`, o que permite
testes de asserção simples:

```go
assertContains(t, resp.IR, "VAR %add_1 int 0")
assertContains(t, resp.IR, "LOOP_BEGIN %stmLoop_1")
assertContains(t, resp.IR, "BREAK_IF %compare_1")
```

Isso é muito mais legível e estável do que testar o código Go gerado
(que pode mudar formatação sem mudar semântica).
