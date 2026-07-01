# ui/mainMenu — Sistema de Menu Hexagonal da IDE

## Arquivos do package

| Arquivo             | Responsabilidade                                                                                                                                             |
|---------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `mainMenuButton.go` | Botão hexagonal fixo no canvas. Renderiza o ícone, gerencia animação de atenção, e abre/fecha o menu ao clicar.                                              |
| `spriteHexMenu.go`  | Motor de renderização. Converte `[]hexMenu.MenuItem` em `sprite.Element` no canvas, gerencia navegação entre páginas (submenu/goBack), backdrop, e tutorial. |
| `menuBuilder.go`    | Define a hierarquia do menu (quais itens, submenus, ícones, labels, callbacks). Único arquivo que você modifica para alterar a estrutura do menu.            |

## Dependências entre packages

```
ui/mainMenu  →  hexMenu        (tipos: MenuItem, Config, renderização SVG)
             →  rulesIcon       (constantes de ícones FontAwesome)
             →  rulesMainMenu   (cores, tamanhos, estilos visuais)
             →  sprite          (renderização no canvas)
```

`menuBuilder.go` **NÃO importa** `factoryDevice`. Em vez disso, define a interface:

```go
type DeviceCreator interface {
    SafeRun(name string, fn func())
    CreateAdd()
    CreateSub()
    CreateMul()
    CreateDiv()
    CreateLoop()
    CreateConstInt()
    CreateGauge()
}
```

`*factoryDevice.DeviceFactory` satisfaz essa interface implicitamente (duck typing do Go).
Isso evita dependência circular entre `ui/mainMenu` e `factoryDevice`.

---

## Como o menu funciona

### Fluxo de execução

1. `stageWorkspace.Init()` cria um `MenuBuilder` e chama `Build()` → retorna `[]hexMenu.MenuItem`
2. Passa os items para `Button.SetMenuItems(items)`
3. `Button.Init()` cria o botão hexagonal no canvas
4. Usuário clica no botão → `SpriteHexMenu.Open(items, x, y)` renderiza os hexágonos
5. Clique em submenu → `navigateToSubmenu()` empilha página atual, renderiza submenu
6. Clique em action → executa `OnClick()`, fecha menu
7. Clique em GoBack → `goBack()` desempilha página anterior

### Grid hexagonal (Col, Row)

Os hexágonos são posicionados em uma grade offset. Valores ímpares de Col e Row:

```
Col=1,Row=1    Col=3,Row=1    Col=5,Row=1
     Col=2,Row=2    Col=4,Row=2
Col=1,Row=3    Col=3,Row=3    Col=5,Row=3
     Col=2,Row=4    Col=4,Row=4
```

**Regra**: use sempre Col e Row ímpares para a coluna principal, pares para posições "entre" hexágonos. O grid usa offset hexagonal — hexágonos em colunas pares ficam deslocados meia altura para baixo.

---

## Como adicionar um novo device ao menu

### Passo 1: Criar o método no factory

Em `factoryDevice/factory.go`, adicione:

```go
func (f *DeviceFactory) CreateMyDevice() {
    stm := new(devices.StatementMyDevice)
    stm.SetStage(f.Stage)
    stm.SetWireManager(f.WireMgr)
    stm.SetResizerButton(f.ResizeButton)
    stm.SetDraggerButton(f.DraggerButton)
    stm.SetGridAdjust(f.GridAdjust)

    if err := stm.Init(); err != nil {
        log.Printf("[Factory] StatementMyDevice.Init: %v", err)
        return
    }

    stm.RegisterConnectors()
    manager.Manager.Register(stm)
    manager.Manager.Register(stm.GetIcon())
    stm.SetOverlapPolicy(scene.OverlapPolicy{
        AllowAbove: false, AllowBelow: true, AllowPartial: false,
    })
    f.SceneMgr.Register(stm)
    stm.SetSceneNotify(f.SceneNotifyFn)

    cx, cy := f.screenCenter()
    stm.SetPosition(cx, cy)
    stm.SetDragEnable(true)
    stm.Append()
    log.Printf("[Factory] Created StatementMyDevice at (%v, %v)", cx, cy)
}
```

### Passo 2: Adicionar à interface DeviceCreator

Em `menuBuilder.go`, adicione o método à interface:

```go
type DeviceCreator interface {
    SafeRun(name string, fn func())
    CreateAdd()
    CreateSub()
    CreateMul()
    CreateDiv()
    CreateLoop()
    CreateConstInt()
    CreateGauge()
    CreateMyDevice()  // ← novo
}
```

### Passo 3: Adicionar o item ao menu

#### Opção A: Adicionar a um submenu existente

Edite o método do submenu em `menuBuilder.go`. Exemplo: adicionar "MyDev" ao submenu Math:

```go
func (b *MenuBuilder) mathSubmenu() []hexMenu.MenuItem {
    styles := rulesMainMenu.MenuStyles()
    back := hexMenu.GoBackItem(3, 3)
    back.Styles = styles

    return []hexMenu.MenuItem{
        back,
        {ID: "Add", Col: 2, Row: 2, Label: "Add", ...},
        {ID: "Sub", Col: 2, Row: 4, Label: "Sub", ...},
        {ID: "Mul", Col: 4, Row: 2, Label: "Mul", ...},
        {ID: "Div", Col: 4, Row: 4, Label: "Div", ...},
        // ↓ NOVO ITEM ↓
        {
            ID:              "MyDev",
            Col:             3,       // posição no grid hex
            Row:             5,       // abaixo dos existentes
            Label:           "MyDev",
            FontAwesomePath: rulesIcon.KFAGear,     // ícone FontAwesome
            ViewBox:         "0 0 512 512",          // viewBox do ícone
            Type:            hexMenu.ItemAction,     // ação (não submenu)
            OnClick:         func() { b.factory.SafeRun("CreateMyDevice", b.factory.CreateMyDevice) },
            Styles:          styles,
        },
    }
}
```

#### Opção B: Criar uma nova categoria no menu principal

Adicione ao `Build()` e crie um novo método de submenu:

```go
func (b *MenuBuilder) Build() []hexMenu.MenuItem {
    styles := rulesMainMenu.MenuStyles()

    return []hexMenu.MenuItem{
        // ... items existentes (Math row=1, Loop row=3, Const row=5, Display row=7) ...

        // ↓ NOVA CATEGORIA ↓
        {
            ID:              "SysMyCategory",
            Col:             1,
            Row:             9,       // próxima posição ímpar disponível
            Label:           "MyCat",
            FontAwesomePath: rulesIcon.KFAGear,
            ViewBox:         "0 0 512 512",
            Type:            hexMenu.ItemSubmenu,
            Submenu:         b.myCategorySubmenu(),  // método novo
            Styles:          styles,
        },

        // Export e Settings movem para baixo
        {ID: "SysExport", Col: 1, Row: 11, ...},
        {ID: "SysSettings", Col: 1, Row: 13, ...},
    }
}

func (b *MenuBuilder) myCategorySubmenu() []hexMenu.MenuItem {
    styles := rulesMainMenu.MenuStyles()
    back := hexMenu.GoBackItem(2, 4)   // botão voltar na posição (2,4)
    back.Styles = styles

    return []hexMenu.MenuItem{
        back,
        {
            ID:              "MyDev",
            Col:             1,
            Row:             3,
            Label:           "MyDev",
            FontAwesomePath: rulesIcon.KFAGear,
            ViewBox:         "0 0 512 512",
            Type:            hexMenu.ItemAction,
            OnClick:         func() { b.factory.SafeRun("CreateMyDevice", b.factory.CreateMyDevice) },
            Styles:          styles,
        },
    }
}
```

---

## Referência: campos de hexMenu.MenuItem

| Campo             | Tipo                 |   Obrigatório   | Descrição                                                                                                                           |
|-------------------|----------------------|:---------------:|-------------------------------------------------------------------------------------------------------------------------------------|
| `ID`              | `string`             |        ✓        | Identificador único. Usado internamente para hit-test, tutorial, e geração de IDs de sprite. Deve ser único dentro da mesma página. |
| `Col`             | `int`                |        ✓        | Posição na coluna do grid hexagonal (1-based).                                                                                      |
| `Row`             | `int`                |        ✓        | Posição na linha do grid hexagonal (1-based).                                                                                       |
| `Label`           | `string`             |        ✓        | Texto exibido abaixo do ícone dentro do hexágono.                                                                                   |
| `FontAwesomePath` | `string`             |        ✓        | Path SVG do ícone FontAwesome. Use constantes de `rulesIcon` (ex: `rulesIcon.KFAPlus`).                                             |
| `ViewBox`         | `string`             |        ✓        | ViewBox SVG do ícone. Cada ícone FA tem seu próprio viewBox — copie do source ou use as constantes.                                 |
| `Type`            | `hexMenu.ItemType`   |        ✓        | `hexMenu.ItemAction` = executa OnClick e fecha menu. `hexMenu.ItemSubmenu` = navega para submenu.                                   |
| `OnClick`         | `func()`             | Só para Action  | Callback executado ao clicar. Para criar devices: `func() { b.factory.SafeRun("Name", b.factory.Method) }`                          |
| `Submenu`         | `[]hexMenu.MenuItem` | Só para Submenu | Items do submenu. Deve incluir um `hexMenu.GoBackItem(col, row)` para voltar.                                                       |
| `Styles`          | `hexMenu.Styles`     |        ✓        | Use `rulesMainMenu.MenuStyles()` para consistência visual.                                                                          |

## Referência: constantes de ícones disponíveis (rulesIcon)

| Constante                  | Ícone | Uso atual                 |
|----------------------------|-------|---------------------------|
| `KFASquareRootVariable`    | √x    | Math (categoria)          |
| `KFAPlus`                  | +     | Add                       |
| `KFAMinus`                 | −     | Sub                       |
| `KFAXmark`                 | ×     | Mul                       |
| `KFADivide`                | ÷     | Div                       |
| `KFARepeat`                | ↻     | Loop                      |
| `KFABars`                  | ☰     | Const, Menu button        |
| `KFAGear`                  | ⚙     | Display, Settings         |
| `KFAFileExport`            | ↗     | Export                    |
| `KFATrashCan`              | 🗑    | Delete (menus de devices) |
| `KFALink`                  | 🔗    | Connect wire              |
| `KFALinkSlash`             | ⛓‍💥  | Disconnect wire           |
| `KFAArrowsUpDownLeftRight` | ↕↔    | Resize                    |

Para adicionar novos ícones, defina a constante SVG path em `rulesIcon/rules.go`.

## Referência: GoBackItem

Todo submenu **deve** incluir um botão de voltar:

```go
back := hexMenu.GoBackItem(col, row)
back.Styles = styles  // obrigatório: aplicar styles
```

O `GoBackItem` usa `ID: "SysGoBack"` que é tratado especialmente pelo `spriteHexMenu.go` — ao clicar, desempilha a página anterior em vez de executar um callback.

**Posicionamento recomendado**: coloque o GoBack na posição que faça sentido visual para o layout do submenu. Para submenus com 1 item na coluna 1: `GoBackItem(2, 4)`. Para submenus 2×2: `GoBackItem(3, 3)`.

## Referência: SafeRun

```go
b.factory.SafeRun("CreateAdd", b.factory.CreateAdd)
```

`SafeRun` executa a função em goroutine separada com:
1. **150ms delay** — permite que o menu `Close()` termine de destruir seus elementos DOM antes de criar novos
2. **Panic recovery** — loga o erro em vez de crashar o WASM

**Sempre use SafeRun** para callbacks que criam elementos no stage. Sem ele, a criação do device e a destruição do menu competem pelos mesmos recursos do canvas.

---

## Hierarquia atual do menu

```
● Menu (botão hexagonal fixo)
├── Math (submenu)
│   ├── [GoBack]
│   ├── Add    (Col=2, Row=2) → factory.CreateAdd()
│   ├── Sub    (Col=2, Row=4) → factory.CreateSub()
│   ├── Mul    (Col=4, Row=2) → factory.CreateMul()
│   └── Div    (Col=4, Row=4) → factory.CreateDiv()
├── Loop (submenu)
│   ├── [GoBack]
│   └── Loop   (Col=1, Row=3) → factory.CreateLoop()
├── Const (submenu)
│   ├── [GoBack]
│   └── Int    (Col=1, Row=3) → factory.CreateConstInt()
├── Display (submenu)
│   ├── [GoBack]
│   └── Gauge  (Col=1, Row=3) → factory.CreateGauge()
├── Export (action) → sceneNotifyFn()
└── Settings (action) → log only
```

## Checklist para adicionar um novo item

- [ ] `factoryDevice/factory.go`: criar método `Create*()`
- [ ] `menuBuilder.go`: adicionar método à interface `DeviceCreator`
- [ ] `menuBuilder.go`: adicionar `hexMenu.MenuItem` ao submenu ou criar novo submenu
- [ ] Verificar que o `ID` é único
- [ ] Verificar que `Col`/`Row` não colide com item existente na mesma página
- [ ] Usar `rulesMainMenu.MenuStyles()` nos Styles
- [ ] Usar `SafeRun` no OnClick
- [ ] Se novo submenu: incluir `GoBackItem` com Styles aplicados
- [ ] Se nova categoria: ajustar `Row` dos itens seguintes (Export, Settings)
