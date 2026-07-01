# Wire Package — Sistema de Conexões para IDE Gráfica

## Arquivos

| Arquivo                | Linhas | Descrição                                                                              |
|------------------------|--------|----------------------------------------------------------------------------------------|
| `doc.go`               | 26     | Documentação do pacote                                                                 |
| `types.go`             | 286    | `ConnectorID`, `ConnectorInfo`, `Wire`, `WireStyle`, `Point`, `Candidate`              |
| `registry.go`          | 164    | Estilos visuais por tipo (`int`→azul, `float`→vermelho...) e matriz de compatibilidade |
| `routing.go`           | 151    | Algoritmo Manhattan com 3 casos (simples, próximo, reverso)                            |
| `renderer.go`          | 235    | Desenho no Canvas 2D com cantos arredondados (`arcTo`) e hit-testing por distância     |
| `manager.go`           | 779    | Orquestrador central: registro de conectores, workflow de conexão, CRUD de wires       |
| `errors.go`            | 49     | Erros tipados                                                                          |
| `integration_guide.go` | 228    | Guia passo-a-passo para integrar com `StatementAdd` e `main.go`                        |

**Total: ~1918 linhas**

## Decisões Implementadas

- ✅ Sem desvio de obstáculos (V1)
- ✅ Conexão via menu → seleção de destino (StartConnect → lista de candidates → FinishConnect)
- ✅ Layer configurável (WireLayerAbove / WireLayerBelow)
- ✅ Deletar via menu Disconnect + clique no wire (HitTest + SelectWire + DeleteWire)
- ✅ Posições hardcoded via `PositionFunc` (closure que lê posição do element)
- ✅ Wires independentes (N wires por output)
- ✅ `AllowedTypes []string` substituiu `DataType string`
- ✅ `Locked` substituiu `LookedUp`

## Como Integrar (Resumo)

### 1. main.go
```go
wireMgr := wire.NewManager()
wireMgr.MarkDirtyFunc = func() { spriteStage.MarkDirty() }

// Após spriteStage.Start():
spriteCtx := spriteDoc.Call("getElementById", "spriteCanvas").Call("getContext", "2d")
wireMgr.SetRenderContext(spriteCtx)

spriteStage.SetRenderCallback(func() {
    wireMgr.Draw()
})
```

### 2. StatementAdd.Init()
```go
// Após criar o sprite.Element, registrar conectores:
stmAdd.RegisterConnectors(wireMgr)
```

### 3. HexMenu "Connect"
```go
candidates := wireMgr.StartConnect(connID)
// Mostrar candidates como menu items
// Ao selecionar: wireMgr.FinishConnect(targetID)
```

### 4. Drag/Resize
```go
// No onDragEnd/onResizeEnd:
wireMgr.RecalculateForElement(e.id)
```

## Estilos Visuais Padrão

| Tipo         | Cor        | Largura | Nota                      |
|--------------|------------|---------|---------------------------|
| `int`        | 🔵 #2196F3 | 2px     | —                         |
| `float`      | 🔴 #F44336 | 2px     | —                         |
| `string`     | 🟢 #4CAF50 | 2px     | —                         |
| `bool`       | 🟠 #FF9800 | 2px     | —                         |
| `[]int`      | 🔵 #2196F3 | **4px** | Array = linha mais grossa |
| `[]float`    | 🔴 #F44336 | **4px** | Array = linha mais grossa |
| Desconhecido | ⚪ #9E9E9E  | 2px     | Tracejado                 |

## Próximos Passos

1. **Integrar** o pacote no projeto e testar a renderização básica
2. **Adaptar hexMenu** para mostrar candidates de conexão
3. **Testar touch** no tablet (ajustar `hitTolerance`)
4. **Futuramente**: desvio de obstáculos, animação de fluxo, bifurcação de wires
