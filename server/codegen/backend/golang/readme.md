# backend/golang — Backend Go do pipeline de codegen

## O que este pacote faz

Recebe um `*ir.Program` e produz uma string com código-fonte Go completo e
compilável — `package main`, imports, structs de black-boxes, e `func main()`.

Este pacote não faz análise. Não ordena, não detecta escopos, não decide onde
declarar variáveis. **Todo esse trabalho foi feito pelo emitter IR.** O backend
apenas traduz cada instrução IR para texto Go, linha por linha.

---

## Por que a separação é importante

Um backend de linguagem deve ser burro por design. Se um backend começa a fazer
análise (decidir se deve usar `var` ou `:=`, reordenar instruções), ele está
duplicando lógica que já existe no IR emitter e criando inconsistências.

A regra: **se o IR diz `VAR`, o backend escreve `var`. Se o IR diz `CONST`,
o backend escreve `:=`. O backend nunca questiona.**

Isso garante que adicionar suporte a uma nova linguagem é somente escrever
um novo arquivo de tradução, sem tocar no IR emitter.

---

## Estrutura de saída

O Go gerado tem sempre a mesma estrutura:

```go
package main

import (
    "fmt"
    "machine"
    "time"
)

// ── Black-box: I2CBus ──

type I2CBus struct { ... }    // ← StructCode do BlackBoxDef

func (s *I2CBus) Init() { }   // ← MethodsCode do BlackBoxDef

// ── Black-box: APDS9960 ──

type APDS9960 struct { ... }
func (s *APDS9960) Init() { }
func (s *APDS9960) Run() { }

func main() {
    // corpo gerado a partir das instruções IR, nesta ordem:
    // VAR (registros promovidos)
    // BB_DECL (var sensor I2CBus)
    // BB_PROP (sensor.field = value)
    // BB_INIT (sensor.Init(...))
    // LOOP_BEGIN / LOOP_END / BREAK_IF
    // BB_RUN (sensor.Run(...))
    // OUTPUT (fmt.Println)
}
```

Duas regiões separadas são construídas em paralelo:

| Região | Builder | Conteúdo |
|---|---|---|
| `topLevel` | `strings.Builder` | Structs e métodos dos black-boxes |
| `body` | `strings.Builder` | Corpo de `func main()` |

`wrapMain()` combina as duas no arquivo final com `package main`, imports, e
a assinatura de `func main()`.

---

## Mapeamentos IR → Go

### Identifiers

O IR usa IDs como `"constInt_1"`. Go não aceita `_` antes de dígito. A função
`goIdent` remove o `_` antes de números:

```
constInt_1  →  constInt1
stmLoop_1   →  stmLoop1
apds9960_1  →  apds99601
```

Isso é necessário porque o Go trata `_` como identificador blank (`_`).
`constInt_1` seria interpretado como `constInt` seguido de `_1` em alguns
contextos.

### Operands (referências a registros)

```go
func goOperand(arg string) string {
    if strings.HasPrefix(arg, "%") {
        ref := arg[1:]
        if idx := strings.Index(ref, ":"); idx >= 0 {
            // Forma composta: %i2cBus_1:bus → i2cBus1_bus
            return goIdent(ref[:idx]) + "_" + ref[idx+1:]
        }
        return goIdent(ref)  // %constInt_1 → constInt1
    }
    return arg  // literal numérico: "10" → "10"
}
```

A forma composta `%instanceId:portName` é usada para saídas de black-boxes
onde um único método retorna múltiplos valores. `i2cBus1_bus` é o nome da
variável Go que recebe o valor da porta `bus` da instância `i2cBus1`.

### Tipos

```go
func goTypeName(irType string) string {
    switch irType {
    case "int":    return "int64"   // ← sempre int64 (não int)
    case "float":  return "float64"
    case "bool":   return "bool"
    case "string": return "string"
    default:       return "int64"
    }
}
```

**Por que `int64` em vez de `int`?**

Microcontroladores como o RP2040 podem usar `int` de 32 bits. Usar `int64`
explicitamente garante comportamento consistente independente da plataforma
de compilação.

### VAR vs. CONST (`:=` vs. `var`)

```go
case ir.OpConst:
    e.writef("%s := %s(%s)\n", name, goType, val)   // curta declaração
case ir.OpVar:
    e.writef("var %s %s\n", name, goType)            // declaração longa
case ir.OpAssign:
    e.writef("%s = %s\n", name, val)                 // atribuição (var já existe)
```

O backend não decide qual usar — o IR já vem com o opcode correto.

---

## Geração de Black-Boxes

### BB_DECL — declaração + código do struct

```go
func (e *goEmitter) emitBBDecl(inst ir.Instruction) {
    structName := inst.Meta["struct"]

    if !e.bbEmitted[structName] {
        // Copia struct e métodos para topLevel (uma vez por tipo)
        def := e.lookupDef(structName)
        e.topLevel.WriteString("// ── Black-box: " + structName + " ──\n\n")
        e.topLevel.WriteString(def.StructCode + "\n\n")
        e.topLevel.WriteString(def.MethodsCode + "\n\n")
        // Adiciona imports do black-box
        for _, imp := range def.Imports {
            e.addImport(imp)
        }
        e.bbEmitted[structName] = true
    }

    // Emite a declaração da variável no corpo de main()
    e.writef("var %s %s\n", goIdent(inst.Dest), structName)
}
```

**Por que `topLevel` e `body` separados?**

Structs em Go devem ser declarados no nível do pacote, não dentro de funções.
Se o backend tentasse misturar as declarações de struct no mesmo builder do
`main()`, precisaria de reordenação. Usando dois builders separados, a ordem
é sempre correta: `topLevel` vem antes de `func main()` no arquivo final.

**Por que `bbEmitted` é necessário?**

O maker pode colocar dois sensores APDS9960 na cena (duas instâncias). O IR
terá dois `BB_DECL` — um para cada instância. Mas o struct `APDS9960` só deve
aparecer uma vez no código gerado. `bbEmitted` rastreia quais tipos de struct
já foram escritos no `topLevel`.

### BB_PROP — propriedades configuradas pelo maker

```go
func (e *goEmitter) emitBBProp(inst ir.Instruction) {
    varName := goIdent(inst.Dest)      // ex: "apds99601"
    field   := inst.Args[0]            // ex: "gain"
    value   := inst.Args[1]            // ex: "0"

    // String props precisam de aspas
    if goType == "string" && !strings.HasPrefix(value, `"`) {
        value = fmt.Sprintf("%q", value)
    }

    e.writef("%s.%s = %s\n", varName, field, value)
    // → apds99601.gain = 0
}
```

### BB_INIT — chamada com múltiplos outputs

```go
// IR: BB_INIT %i2cBus_1 {struct=I2CBus}
// Outputs do Init conforme BlackBoxDef: [{name:"bus"}, {name:"err", isError:true}]
// Go: i2cBus1_bus, i2cBus1_err := i2cBus1.Init()
//     _ = i2cBus1_err   ← erro suprimido se não conectado
```

O backend lê `def.Init.Outputs` do `BlackBoxDef` para saber quantas variáveis
declarar no lado esquerdo da atribuição. Outputs de erro (`isError: true`) são
automaticamente suprimidos com `_ = varName` para evitar erros de compilação Go
("declared but not used").

---

## Imports — coleta e deduplicação

```go
type goEmitter struct {
    imports map[string]bool  // set de import paths
}

func (e *goEmitter) addImport(path string) {
    e.imports[path] = true
}
```

Imports são coletados de duas fontes:
1. Instruções nativas (`OUTPUT` → `"fmt"`)
2. Black-box defs (`def.Imports` → `["machine", "time"]`)

`wrapMain()` usa um mapa para deduplicar e sorteia os imports para saída
determinística.

---

## `wrapMain` — montagem do arquivo final

```go
func (e *goEmitter) wrapMain() string {
    var sb strings.Builder
    sb.WriteString("package main\n\n")

    // 1. Imports
    if len(e.imports) == 1 { /* import único */  }
    else                   { /* import block */  }

    // 2. Top-level (structs e métodos de black-boxes)
    if e.topLevel.Len() > 0 {
        sb.WriteString(e.topLevel.String())
    }

    // 3. func main() { body }
    sb.WriteString("func main() {\n")
    sb.WriteString(e.body.String())
    sb.WriteString("}\n")

    return sb.String()
}
```

---

## Tratamento do `declared` map

```go
declared map[string]bool  // variáveis já declaradas
```

Um node pode ser promovido a `VAR` no escopo global mas também aparecer com
`ASSIGN` dentro do loop. O backend usa `declared` para saber se deve escrever
`:=` (primeira declaração) ou `=` (reatribuição):

```go
func (e *goEmitter) emitBinOp(inst ir.Instruction) {
    if e.declared[inst.Dest] {
        e.writef("%s = %s %s %s\n", name, a, op, b)   // já declarado → =
    } else {
        e.writef("%s := %s %s %s\n", name, a, op, b)  // primeira vez → :=
        e.declared[inst.Dest] = true
    }
}
```

---

## Adicionando um novo tipo de device

Para adicionar um novo device nativo ao codegen:

1. **WASM side:** criar o device em `devices/`, implementar `GetDeviceType()`
	 retornando o novo type string (ex: `"StatementMod"`).

2. **IR emitter** (`ir/emit.go`): adicionar case em `emitNode`:
	 ```go
	 case node.Type == "StatementMod":
			 e.emitBinOp(node, OpMod)  // ou um opcode novo
	 ```

3. **IR types** (`ir/types.go`): se necessário, adicionar novo `Op`:
	 ```go
	 OpMod Op = "MOD"
	 ```

4. **Go backend** (`backend/golang/emit.go`): adicionar case no switch principal:
	 ```go
	 case ir.OpMod:
			 e.emitBinOp(inst)
	 ```
	 E adicionar o operador em `goBinOp`:
	 ```go
	 case ir.OpMod:
			 return "%"
	 ```

5. **Validação** (`codeGen.go`): adicionar o novo type na lista de devices
	 que precisam de inputs conectados.
