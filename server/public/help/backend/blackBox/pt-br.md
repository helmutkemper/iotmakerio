# IoTMaker Doc Standard (IDS) — Referência Completa

> Padrão de documentação para componentes **black-box** na IDE IoTMaker.
> Totalmente compatível com `go doc` — as tags IDS vivem dentro de comentários
> Go normais e são invisíveis para todas as ferramentas padrão do Go.

---

## O que é um black-box?

Um **black-box** é um struct Go que um especialista escreve uma vez, publica no
GitHub, e que qualquer maker pode arrastar para o canvas da IDE como um bloco
visual pronto para conectar — sem ler uma única linha de código.

A IDE renderiza o struct como um retângulo arredondado escuro com pinos
conectores rotulados em cada lado. Pinos do lado esquerdo são entradas
(parâmetros do método); pinos do lado direito são saídas (valores de retorno).
O maker conecta com fios virtuais e a IDE gera código Go válido automaticamente.

---

## Início rápido — o black-box mais simples válido

```go
// Package mydevice
//
// Sum adds two integers.
package mydevice

// Sum is a stateless adder with no hardware setup required.
type Sum struct{}

// Run returns the sum of a and b.
//
// Params
//   a: first operand.   connection:mandatory.
//   b: second operand.  connection:mandatory.
//
// Returns
//   c: a + b.  connection:mandatory.
func (s *Sum) Run(a, b int) (c int) {
	return a + b
}
```

Publique como release no GitHub, envie a URL na IDE, e o bloco "Sum" aparece
em **Hardware → Sum → Run** em qualquer canvas.

---

## Layout do repositório

```
seu-repo/
├── my_sensor.go          ← Código Go com UM struct exportado + tags IDS
├── readme.md             ← Visão geral do device (inglês, auto-detectado)
├── readme.pt-br.md       ← Visão geral do device (português)
├── init.en.md            ← Aba de ajuda do Init (inglês)
├── init.pt-br.md         ← Aba de ajuda do Init (português)
├── run.en.md             ← Aba de ajuda do Run (inglês)
├── rp2040.svg            ← Diagrama interativo (opcional)
└── wiring.png            ← Imagem normal (opcional)
```

Regras:
- Exatamente **um struct exportado** por arquivo `.go`.
- Pelo menos **um** entre `Init()` ou `Run()` deve existir.
- Todos os parâmetros e valores de retorno devem usar **tipos nativos Go**.
- Todo parâmetro e valor de retorno **deve** ter a tag `connection:`.
- Todos os arquivos ficam na **raiz** do repositório (o parser encontra lá).

---

## Métodos

### Init()

`Init()` representa **configuração única**: adquirir um barramento, configurar
registradores, abrir uma conexão. O gerador de código coloca no escopo global
para rodar **antes** do loop principal.

**Importante:** se `Init()` existe no componente, o maker **deve** colocar um
device Init no canvas. O gerador retorna erro se um device Run está presente
mas o Init correspondente está faltando.

```go
// Init configures the sensor on the given I2C bus.
//
// executionOrder:10. icon:hourglass-start. label:Init.
//
// Params
//   i2c: I2C bus reference.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialisation error.  connection:optional.
func (s *MySensor) Init(i2c *machine.I2C) (err error) { ... }
```

### Run()

`Run()` representa **trabalho por iteração**: ler um sensor, escrever uma saída,
calcular um valor. O maker arrasta para dentro de um bloco Loop.

```go
// Run reads the sensor and returns the measured value.
//
// icon:bolt. label:Read.
//
// Returns
//   value: measured value.  range:0..4095.  connection:mandatory.
//   err:   read error.      connection:optional.
func (s *MySensor) Run() (value uint16, err error) { ... }
```

### Devices só com Run (sem Init)

Um device com **apenas** `Run()` e um struct vazio é válido. Use para funções
puras — operações matemáticas, conversões, codificadores — que não precisam
de estado.

---

## Posicionamento da declaração de variável

O gerador de código aplica uma regra simples para onde `var device X` aparece:

| Condição                                                      | Posição do var                      |
|---------------------------------------------------------------|-------------------------------------|
| **Qualquer** método colocado **fora** do loop (escopo global) | Topo do `main()`, **antes** do loop |
| **Todos** os métodos colocados **dentro** do loop             | Topo do corpo do loop               |

Essa regra é automática — o especialista não precisa configurar.

---

## Ordem de execução

Por padrão, devices são ordenados por **dependência de fio**: se a saída do
device A está conectada à entrada do device B, A roda antes de B. Quando dois
devices não compartilham fio, a ordem é indefinida.

Para casos onde a ordem importa mas nenhum fio conecta os devices, use
`executionOrder:`:

```go
// Init configures the I2C bus.
// executionOrder:1.
func (b *I2CBus) Init() (bus *machine.I2C, err error) { ... }

// Init configures the sensor using the I2C bus.
// executionOrder:2.
func (s *MySensor) Init(i2c *machine.I2C) (err error) { ... }
```

| Situação                    | Resultado                                |
|-----------------------------|------------------------------------------|
| Ambos com `executionOrder:` | Número menor roda primeiro               |
| Apenas um tem               | O ordenado roda primeiro                 |
| Nenhum tem                  | Dependência de fio, depois ID alfabético |
| Números iguais              | Desempate por ID alfabético              |

`executionOrder:` se aplica por método. Se um componente tem `Init()` e
`Run()`, cada um carrega seu próprio valor de ordem independentemente.

---

## Referência de tags IDS

Tags são escritas em comentários `//` na seção `Params` ou `Returns` de um
método, na mesma linha que a descrição do parâmetro.

```go
// Params
//   paramName: description.  tag1:value.  tag2:value.
```

### Regras de sintaxe

| Regra                       | Detalhe                                                            |
|-----------------------------|--------------------------------------------------------------------|
| Seções `Params` / `Returns` | Uma linha contendo apenas a palavra (sem dois-pontos) abre a seção |
| Formato da tag              | chave `camelCase` + `:` + valor, seguido de `.` ou fim da linha    |
| Ordem das tags              | Livre — o parser identifica tags por prefixo, não por posição      |
| Compatibilidade `go doc`    | Tags aparecem como texto simples; nenhuma ferramenta quebra        |

### Tags de porta (em parâmetros e valores de retorno)

| Tag           | Sintaxe                | Obrigatória         | Descrição                                            |
|---------------|------------------------|---------------------|------------------------------------------------------|
| `connection:` | `connection:mandatory` | **Sim, toda porta** | `mandatory` ou `optional`. Ausente = aviso do parser |
| `range:`      | `range:0..255`         | Não                 | Intervalo numérico fechado                           |
| `range_min:`  | `range_min:0`          | Não                 | Limite inferior apenas                               |
| `range_max:`  | `range_max:100`        | Não                 | Limite superior apenas                               |
| `unit:`       | `unit:ms`              | Não                 | Unidade física; IDE avisa em conexões incompatíveis  |
| `default:`    | `default:128`          | Não                 | Valor usado quando a porta está desconectada         |
| `options:`    | `options:a,b,c`        | Não                 | Enum — mostra dropdown na IDE                        |
| `encoding:`   | `encoding:bitmask`     | Não                 | `bitmask` ou `tristate`                              |
| `bits:`       | `bits:0..3`            | Não                 | Fatia de bits dentro de um inteiro maior             |

### Diretivas de método (no comentário do método, antes de Params)

| Diretiva          | Sintaxe                | Descrição                                                  |
|-------------------|------------------------|------------------------------------------------------------|
| `executionOrder:` | `executionOrder:1`     | Posição relativa de execução dentro de um escopo           |
| `icon:`           | `icon:hourglass-start` | Nome do ícone FontAwesome (kebab-case)                     |
| `label:`          | `label:Init`           | Nome legível do bloco                                      |
| `menu:`           | `menu:-1,-1`           | Posição explícita no menu hexagonal, offset do centro Back |

### Diretivas de struct (no comentário do struct)

| Diretiva       | Sintaxe               | Descrição                                                       |
|----------------|-----------------------|-----------------------------------------------------------------|
| `icon:`        | `icon:gear`           | Ícone FontAwesome do componente                                 |
| `label:`       | `label:APDS9960`      | Nome legível do componente                                      |
| `interactive:` | `interactive:rp2040.` | Nome do arquivo SVG sem extensão (veja "Diagramas interativos") |

---

## Propriedades configuráveis — struct tag `prop`

Campos do struct com tag `prop` aparecem no **painel de inspeção** do device
Init. Não são pinos conectores — o maker digita ou seleciona um valor.

```go
type MySensor struct {
    addr    uint8  `prop:"I2C Address"       default:"0x39"  options:"0x39,0x29"`
    gain    byte   `prop:"Gain"              default:"0"     options:"0,1,2,3"`
    intTime byte   `prop:"Integration Time"  default:"255"`
}
```

| Tag do campo        | Função                                                        |
|---------------------|---------------------------------------------------------------|
| `prop:"Label"`      | Nome legível mostrado no painel de inspeção                   |
| `default:"valor"`   | Valor pré-preenchido                                          |
| `options:"a,b,c"`   | Renderiza um dropdown em vez de campo texto                   |
| `connection:"ROLE"` | Liga a prop a um diagrama SVG interativo (veja próxima seção) |

---

## Diagramas interativos

Um diagrama SVG interativo pode visualizar o efeito das mudanças de
propriedades. Quando o maker seleciona um pino, o diagrama destaca o elemento.

### Ativação

1. Crie um SVG seguindo a Especificação de Diagrama Interativo
   (veja `docs/INTERACTIVE_DIAGRAM_SPEC.md`).
2. Coloque o SVG na raiz do repositório (ex: `rp2040.svg`).
3. Adicione a diretiva `interactive:` no comentário do struct:

```go
// RP2040_I2C configures I2C on a Raspberry Pi Pico.
//
// icon:microchip. label:RP2040 I2C. interactive:rp2040.
type RP2040_I2C struct {
    sda  string `prop:"SDA Pin"   default:"GP4" options:"GP0,GP2,GP4,GP6" connection:"I2C_SDA"`
    scl  string `prop:"SCL Pin"   default:"GP5" options:"GP1,GP3,GP5,GP7" connection:"I2C_SCL"`
    freq int    `prop:"Frequency" default:"100000" options:"100000,400000"`
}
```

### Como `connection:"ROLE"` funciona

- `ROLE` mapeia para uma cor no atributo `data-palette` do SVG.
- O **valor** da prop (o que o maker seleciona, ex: `"GP4"`) mapeia para o
  atributo `data-id` de um elemento SVG.
- Quando o maker muda o valor, o diagrama destaca o elemento selecionado com a
  cor do role e escurece todos os outros.
- Props **sem** `connection:` (como `freq` acima) não são ligadas ao diagrama —
  aparecem como campos texto/dropdown normais.

### Referência do SVG no markdown

Referencie o SVG em qualquer arquivo markdown de ajuda:

```markdown
![](rp2040.svg)
```

O worker reescreve o caminho para uma URL pública automaticamente.

Para detalhes completos de criação de SVGs, veja `docs/DIAGRAM_CREATION_GUIDE.md`.

---

## Arquivos markdown de ajuda

A documentação para o painel de inspeção da IDE e o menu Hardware é escrita como
arquivos markdown padrão na raiz do repositório.

### Nomenclatura de arquivos

| Padrão                   | Função                                         | Exemplo                        |
|--------------------------|------------------------------------------------|--------------------------------|
| `readme.md`              | Visão geral do device (inglês, auto-detectado) | Descrição no menu Hardware     |
| `readme.{lang}.md`       | Visão geral (outro idioma)                     | `readme.pt-br.md`              |
| `init.{lang}.md`         | Aba de ajuda do Init                           | `init.en.md`, `init.pt-br.md`  |
| `run.{lang}.md`          | Aba de ajuda do Run                            | `run.en.md`                    |
| `{method}.{lang}.md`     | Aba de ajuda de qualquer método                | `read.en.md`                   |
| `{method}.{N}.{lang}.md` | Abas adicionais (numeradas)                    | `init.1.en.md`, `init.2.en.md` |

Códigos de idioma seguem BCP-47 minúsculo: `en`, `pt-br`, `fr`, `ja`, etc.

### Ordenação

Quando um método tem múltiplos arquivos markdown, eles aparecem como sub-abas:

- O arquivo sem número (ex: `init.en.md`) é sempre a primeira aba.
- Arquivos numerados (ex: `init.1.en.md`, `init.2.en.md`) seguem em ordem crescente.

### Título da aba

O título mostrado na sub-aba é extraído do primeiro `# Heading` do arquivo
markdown. Se não houver heading, o nome do arquivo é usado como fallback.

### Imagens no markdown

Referencie imagens da mesma raiz do repositório:

```markdown
![Foto da conexão](wiring.png)
![Diagrama da placa](rp2040.svg)
```

O worker reescreve nomes de arquivo para URLs públicas. Imagens renderizam
inline na aba Help. Todas são clicáveis — clicar abre um lightbox fullscreen.

SVGs interativos (referenciados via diretiva `interactive:`) são pós-processados
automaticamente: elementos são destacados/escurecidos com base nos valores atuais
das props.

### Resolução de idioma

A IDE seleciona qual idioma exibir usando esta prioridade:

1. Preferência da sessão (definida pelo seletor de idioma na aba Help)
2. Preferência de locale do SPA (localStorage `"locale"`)
3. Idioma do navegador (`navigator.language`)
4. Fallback: `"en"`

---

## Painel de controle embutido

Por padrão, o painel de inspeção tem duas abas: **Properties** (campos do
formulário) e **Help** (documentação markdown). O especialista pode unificar
ambos em um fluxo guiado colocando este comentário HTML no markdown de ajuda:

```markdown
# Configuração

Configure os pinos I2C e a frequência abaixo.

<!-- place_the_control_panel_here -->

## Placa RP2040

Quando você muda a configuração dos pinos e aperta Aplicar, o diagrama atualiza.

![](rp2040.svg)
```

### Comportamento

Quando a IDE detecta `place_the_control_panel_here` dentro de um comentário HTML:

- A **aba Properties desaparece**.
- Os campos do formulário (Label, inputs das props, botão Apply) renderizam
  **inline** na posição do placeholder, dentro de um container com borda.
- O maker vê um fluxo único: ler docs → configurar → ver diagrama atualizar.

### Sem o placeholder

Quando nenhum arquivo de ajuda contém o placeholder, o painel de inspeção
mantém o layout normal com duas abas (Properties + Help).

### Preview no menu Hardware

No menu Hardware (antes de colocar o componente no canvas), o mesmo placeholder
é substituído por um **preview estático desabilitado** das props — mostrando
labels e valores padrão em inputs disabled. Sem botão Apply.

---

## Páginas de manual legadas (blocos `/* */` inline)

Componentes que antecedem o sistema de markdown do GitHub podem embutir
documentação diretamente no código Go usando blocos `/* */`. Este sistema ainda
funciona como fallback quando não há arquivos markdown presentes.

### Formato do bloco

```go
/*
manualName:wiring-guide.
language:en.
showIn:init.
` ` `markdown
# Wiring Guide

Conecte **SDA** ao GP4 e **SCL** ao GP5.
` ` `*/
```

(Nota: os backticks acima estão com espaços para renderização — no código real
não têm espaços.)

### Diretivas

| Diretiva      | Obrigatória | Padrão | Descrição               |
|---------------|-------------|--------|-------------------------|
| `manualName:` | **Sim**     | —      | Identificador da página |
| `language:`   | Não         | `en`   | Código de idioma BCP-47 |
| `showIn:`     | Não         | `both` | `init`, `run` ou `both` |

### Quando usar

- **Componentes novos:** use arquivos markdown (`init.en.md`, etc.) — mais
  simples, fácil de editar, suporta imagens e diagramas interativos.
- **Componentes legados:** blocos `/* */` inline ainda funcionam e aparecem na
  aba Help junto com abas markdown. Não é necessário migrar.

---

## Painel de inspeção

O painel de inspeção do **device Init** mostra:

| Campo         | Editável | Origem                              |
|---------------|----------|-------------------------------------|
| Label         | Sim      | Label definido pelo maker no canvas |
| Campos `prop` | Sim      | Campos do struct com tag `prop`     |

O painel de inspeção do **device Run** mostra apenas o campo Label.

A **aba Help** (ou help embutido ao usar o placeholder do painel de controle)
mostra documentação markdown e diagramas interativos.

---

## Fluxo de trabalho

1. Escreva seu componente Go seguindo as regras IDS.
2. Adicione arquivos markdown de ajuda e diagrama SVG interativo (opcional).
3. Crie uma release no GitHub com uma tag de versão.
4. Na IDE, vá em **Projects** e envie a URL da release do GitHub.
5. O worker baixa, analisa e cria os blocos visuais.
6. Encontre seu componente em **Hardware → NomeDoComponente** no menu da IDE.
7. Escolha **Init** ou **Run** no submenu para colocar o bloco desejado.
8. Conecte os pinos a outros devices e gere o código.

---

## Símbolos dos pinos

| Símbolo | Significado                                                    |
|---------|----------------------------------------------------------------|
| ◉       | Conexão obrigatória — deve ser conectado antes de gerar código |
| ◎       | Conexão opcional — pode ficar desconectado                     |
| ⚠       | Tag `connection:` ausente — aviso do parser                    |

---

## Exemplo completo

```go
// Package blackbox
//
// APDS9960 is a colour, proximity, and gesture sensor connected via I2C.
package blackbox

import "machine"

// APDS9960 reads colour (RGBC) data from an I2C bus.
//
// icon:lightbulb. label:APDS9960. interactive:rp2040.
type APDS9960 struct {
    sda   string `prop:"SDA Pin"          default:"GP4" options:"GP0,GP2,GP4,GP6" connection:"I2C_SDA"`
    scl   string `prop:"SCL Pin"          default:"GP5" options:"GP1,GP3,GP5,GP7" connection:"I2C_SCL"`
    gain  byte   `prop:"ADC Gain"         default:"0"   options:"0,1,2,3"`
    atime byte   `prop:"Integration Time" default:"255"`
}

// Init configures the APDS-9960 sensor on the given I2C bus.
//
// executionOrder:10. icon:hourglass-start. label:Init.
//
// Params
//   i2c: I2C bus.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialisation error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {
    return nil
}

// Run reads the four RGBC colour channels from the sensor.
//
// icon:bolt. label:Read.
//
// Returns
//   clear: unfiltered light.  range:0..65535.  connection:optional.
//   red:   red channel.       range:0..65535.  connection:optional.
//   green: green channel.     range:0..65535.  connection:optional.
//   blue:  blue channel.      range:0..65535.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
    return
}
```

Repositório com arquivos de ajuda:

```
APDS9960/
├── apds9960.go
├── readme.md             ← "APDS9960 — Colour & Gesture Sensor"
├── init.en.md            ← guia de conexão + <!-- place_the_control_panel_here --> + ![](rp2040.svg)
├── init.pt-br.md         ← mesmo em português
├── run.en.md             ← como ler valores de cor
└── rp2040.svg            ← diagrama interativo da placa
```

Código gerado:

```go
var apds99601 APDS9960
apds99601.sda = "GP4"
apds99601.scl = "GP5"
apds99601.gain = 0
apds99601.atime = 255
_ = apds99601.Init(i2cBus1_bus)

for {
    apds99601_clear, apds99601_red, _, _ := apds99601.Run()
    ...
}
```

---

## Solução de problemas

| Sintoma                                 | Causa provável                                          | Correção                                       |
|-----------------------------------------|---------------------------------------------------------|------------------------------------------------|
| Componente não aparece no menu Hardware | Erro de parse                                           | Verifique logs/avisos do worker                |
| Sem pinos no componente                 | `Params`/`Returns` escrito errado                       | Verifique capitalização: `Params`, `Returns`   |
| Erro codegen: "Init block missing"      | Componente tem `Init()` mas só Run foi colocado         | Adicione um device Init ao canvas              |
| Dois Init na ordem errada               | Sem fio e sem `executionOrder:`                         | Adicione `executionOrder:N` a cada método Init |
| Diagrama não destaca                    | Role de `connection:` não está no `data-palette` do SVG | Alinhe nomes dos roles entre struct tags e SVG |
| Aba Help vazia                          | Sem arquivos markdown na raiz do repo                   | Adicione `init.en.md` e/ou `readme.md`         |
| Help mostra idioma errado               | Locale do navegador sobrescreve                         | Defina o idioma nas preferências do SPA        |
| Painel embutido não aparece             | Comentário HTML com espaços extras                      | OK — a IDE lida com variações de espaço        |
