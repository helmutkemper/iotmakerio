# IoTMaker Doc Standard (IDS)

> Padrão de documentação para componentes **black-box** da IoTMaker IDE.
> Compatível com `go doc` — estende os comentários Go com tags inline sem quebrar nenhuma ferramenta existente.

---

## 1. Estrutura geral de um comentário de função

```go
// NomeDaFunção faz X. (linha de resumo — obrigatória, usada pelo go doc)
//
// Parágrafo de descrição longa opcional.
// Pode ter múltiplas linhas; segue as regras do godoc.
//
// Pode usar Markdown para formatação.
//
// Params
//   param1: descrição.  tag1:valor.  tag2:valor.
//   param2: descrição.  tag1:valor.
//
// Returns
//   ret1: descrição.  tag1:valor.
//   ret2: descrição.  connection:optional.
func (s *Device) NomeDaFunção(param1 tipo, param2 tipo) (ret1 tipo, ret2 tipo) {}
```

### Regras de sintaxe

| Regra                       | Detalhe                                                      |
|-----------------------------|--------------------------------------------------------------|
| Seções `Params` / `Returns` | Iniciam uma linha contendo apenas a palavra, sem dois-pontos |
| Entrada de parâmetro        | `nome: descrição.  tag:valor.  tag:valor.`                   |
| Tags                        | Sempre `chave:valor` terminado em `.` ou fim de linha        |
| Ordem das tags              | Livre — o parser identifica por prefixo, não por posição     |
| Compatibilidade             | `go doc` ignora as tags e exibe o texto normalmente          |

---

## 2. Tabela completa de tags

| Tag           | Sintaxe                   | Uso                                                                                                   |
|---------------|---------------------------|-------------------------------------------------------------------------------------------------------|
| `range:`      | `min..max`                | Restringe valores numéricos a um intervalo fechado                                                    |
| `range_min:`  | `valor`                   | Apenas limite inferior (sem limite superior)                                                          |
| `range_max:`  | `valor`                   | Apenas limite superior (sem limite inferior)                                                          |
| `unit:`       | texto                     | Unidade de medida informativa                                                                         |
| `options:`    | `a\|b\|c`                 | Enum: valores aceitos listados explicitamente                                                         |
| `default:`    | valor                     | Valor padrão sugerido quando o pino não está conectado                                                |
| `connection:` | `optional` \| `mandatory` | **Obrigatória.** Define se o pino pode ficar desconectado na IDE. O parser acusa erro quando ausente. |
| `encoding:`   | esquema                   | Como interpretar o tipo base (ex: `tristate`, `bitmask`)                                              |
| `bits:`       | `N` ou `N..M`             | Quantos bits do valor são usados / qual fatia                                                         |

---

## 3. Tipos nativos Go — exemplos

### `bool`

Uso mais simples, sem tags especiais necessárias.

```go
// SetEnable ativa ou desativa o componente.
//
// Params
//   enabled: true = ligado, false = desligado.  default:false.
//
// Returns
//   ok: true se o comando foi aceito.  connection:optional.
func (s *Device) SetEnable(enabled bool) (ok bool) {}
```

---

### `int` / `int8` / `int16` / `int32` / `int64`

#### Intervalo numérico simples

```go
// SetVolume ajusta o volume de saída.
//
// Params
//   level: nível de volume.  range:0..100.  unit:percent.  default:50.
func (s *Speaker) SetVolume(level int) {}
```

#### `int` como booleano tristate (`encoding:tristate`)

Quando o domínio do problema usa três estados semânticos mapeados em inteiro:

```go
// SetState define o estado lógico do pino.
//
// Params
//   state: estado lógico.  encoding:tristate.  options:-1|0|1.
//          -1 = false, 0 = undefined (high-impedance), 1 = true.
//
// Returns
//   err: erro de escrita.  connection:optional.
func (s *GPIO) SetState(state int) (err error) {}
```

> **Por que `encoding:tristate` e não apenas `options:`?**
> A IDE trata `tristate` como tipo especial: exibe um seletor de três posições
> em vez de um dropdown genérico, e valida que apenas os valores -1, 0 e 1
> são aceitos, mesmo que a conexão venha de outro pino.

#### `int` como bitmask

```go
// SetFlags configura comportamentos combinados via bitmask.
//
// Params
//   flags: combinação de bits de configuração.  encoding:bitmask.  bits:3.
//          bit0 = enable, bit1 = invert, bit2 = latch.
func (s *Device) SetFlags(flags int) {}
```

> `bits:3` informa que apenas os 3 bits menos significativos são usados.
> A IDE pode exibir um editor de checkboxes no painel de inspeção.

---

### `uint` / `uint8` / `uint16` / `uint32` / `uint64`

Sempre não-negativos; `range:` parte de 0 por convenção.

```go
// SetBrightness ajusta o brilho do LED.
//
// Params
//   value: intensidade do brilho.  range:0..255.  unit:pwm.  default:128.
func (s *LED) SetBrightness(value uint8) {}
```

```go
// ReadADC lê o valor bruto do conversor analógico-digital.
//
// Returns
//   raw: leitura do ADC de 12 bits.  range:0..4095.  unit:adc_counts.
func (s *ADC) ReadADC() (raw uint16) {}
```

---

### `float32` / `float64`

Suporta `range:` com decimais e `unit:` para grandezas físicas.

```go
// ReadTemperature lê a temperatura ambiente.
//
// Returns
//   celsius: temperatura em graus Celsius.  range:-40.0..125.0.  unit:celsius.
//   ok: true se a leitura é válida.  connection:optional.
func (s *TempSensor) ReadTemperature() (celsius float32, ok bool) {}
```

```go
// SetAngle move o servo para um ângulo específico.
//
// Params
//   deg: ângulo em graus.  range:0.0..180.0.  unit:degrees.  default:90.0.
func (s *Servo) SetAngle(deg float64) {}
```

---

### `byte` (alias de `uint8`)

```go
// WriteRegister escreve um valor em um registrador I2C.
//
// Params
//   reg:  endereço do registrador.  range:0x00..0xFF.  unit:hex.
//   data: valor a escrever.         range:0x00..0xFF.  unit:hex.
func (s *I2CDevice) WriteRegister(reg, data byte) {}
```

---

### `string`

```go
// SetLabel define o texto exibido no display.
//
// Params
//   text: texto a exibir.  range_max:20.  unit:chars.  default:"IoTMaker".
func (s *Display) SetLabel(text string) {}
```

> `range_max:20` indica comprimento máximo da string.

---

### `[]byte` (slice de bytes)

```go
// Write envia um buffer de dados via SPI.
//
// Params
//   buf: dados a transmitir.  range_min:1.  range_max:512.  unit:bytes.
//
// Returns
//   n:   número de bytes efetivamente enviados.  connection:optional.
//   err: erro de transmissão.                    connection:optional.
func (s *SPI) Write(buf []byte) (n int, err error) {}
```

> `range_min:1` em um slice indica que o buffer não pode ser vazio.

---

### `error`

O tipo `error` quase sempre é saída. Use `connection:optional` quando o usuário
pode ignorar o erro na IDE, ou `connection:mandatory` quando o erro é crítico
e a IDE deve bloquear execução se o pino estiver desconectado.

```go
// Returns
//   err: erro de inicialização.  connection:mandatory.  ← IDE bloqueia se desconectado
//   err: erro de leitura.        connection:optional.   ← pode ignorar
```

---

### Ponteiros (`*T`)

#### Ponteiro **não pode ser nil** — `nonil:`

Usado quando a função **exige** uma conexão real (ex: barramento I2C já inicializado).
A IDE exibe o pino com borda sólida e impede que o usuário deixe desconectado.

```go
// Init inicializa o sensor no barramento I2C.
//
// Params
//   i2c: referência ao barramento I2C.  connection:mandatory.
//        Conecte à saída do bloco I2CBus.Init.
//
// Returns
//   err: erro de inicialização.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {}
```

> **`nonil:` não tem valor** — a presença da tag já é suficiente.
> O parser trata `nonil:` (com dois-pontos e sem nada após) como flag booleana.

#### Ponteiro **pode ser nil** — comportamento padrão

Quando nil é um valor válido (parâmetro opcional), não use nenhuma tag extra.
Opcionalmente, documente o comportamento:

```go
// Params
//   cfg: configuração opcional.  connection:optional.
//        Se nil, usa os valores padrão internos.
func (s *Device) Configure(cfg *Config) {}
```

---

### Tipos de barramento (`*machine.I2C`, `*machine.SPI`, etc.)

Por convenção, todo parâmetro de barramento deve ter `nonil:` e deve referenciar
de onde a conexão vem:

```go
// Params
//   i2c: barramento I2C.  connection:mandatory.  unit:i2c_bus.
//   spi: barramento SPI.  connection:mandatory.  unit:spi_bus.
```

A tag `unit:i2c_bus` / `unit:spi_bus` permite que a IDE filtre conexões:
um pino `unit:spi_bus` só aceita conexão vinda de um pino também `unit:spi_bus`.

---

## 4. Exemplo completo — APDS-9960

```go
// Package blackbox
//
// APDS9960 — Sensor de cor, proximidade e gestos via I2C.
//
// O componente expõe dois blocos na IDE:
//   - Init: configura o sensor (chamar uma vez no setup)
//   - Run:  lê os canais de cor RGBC (chamar no loop)
package blackbox

import "machine"

// APDS9960 is a color/proximity sensor connected via I2C.
type APDS9960 struct {
    i2c   *machine.I2C
    gain  byte `prop:"Gain"             default:"0"   options:"0,1,2,3"`
    atime byte `prop:"Integration Time" default:"255" range:"0,255"`
}

// Init configures the APDS-9960 sensor.
//
// Params
//   i2c: barramento I2C ao qual o sensor está conectado.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: erro de inicialização.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {
    // ...
    return nil
}

// Run reads the four RGBC color channels from the sensor.
//
// Returns
//   clear: canal de luz total (sem filtro).  range:0..65535.  unit:lux_counts.   connection:optional.
//   red:   canal vermelho.                   range:0..65535.  unit:color_counts.  connection:optional.
//   green: canal verde.                      range:0..65535.  unit:color_counts.  connection:optional.
//   blue:  canal azul.                       range:0..65535.  unit:color_counts.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
    // ...
    return
}
```

---

## 5. Exemplo completo — LED RGB com tristate

```go
// RGBLed controls an RGB LED with individual channel intensity.
type RGBLed struct {
    invert bool `prop:"Invert Logic" default:"false"`
}

// SetColor sets the RGB color of the LED.
//
// Params
//   r: intensidade do canal vermelho.  range:0..255.  unit:rgb.  default:0.
//   g: intensidade do canal verde.     range:0..255.  unit:rgb.  default:0.
//   b: intensidade do canal azul.      range:0..255.  unit:rgb.  default:0.
//
// Returns
//   err: erro de escrita no hardware.  connection:optional.
func (s *RGBLed) SetColor(r, g, b int) (err error) {}

// SetEnabled controla o estado de habilitação do LED.
//
// Params
//   state: estado lógico do LED.  encoding:tristate.  options:-1|0|1.
//          -1 = forçado desligado, 0 = indefinido (mantém estado anterior), 1 = forçado ligado.
func (s *RGBLed) SetEnabled(state int) {}
```

---

## 6. Tag `connection:` — obrigatória

A tag `connection:` é a **única tag obrigatória** do padrão IDS.
O parser acusa aviso de erro quando ela está ausente em qualquer pino.

| Valor                  | Símbolo visual | Significado                                                    |
|------------------------|----------------|----------------------------------------------------------------|
| `connection:optional`  | ◎              | O pino pode ficar desconectado. A IDE não bloqueia execução.   |
| `connection:mandatory` | ◉              | O pino deve estar conectado. A IDE bloqueia execução se vazio. |

### Por que obrigatória?

Sem essa tag, a IDE não sabe como tratar um pino desconectado — se deve gerar erro,
usar um valor default ou simplesmente ignorar. Tornar `connection:` obrigatória
elimina essa ambiguidade e reduz bugs silenciosos no diagrama.

### Legenda automática

Todo dispositivo gerado exibe abaixo dos pinos:

```
◎ conexão opcional    ◉ conexão obrigatória
```

### Exemplos

```go
// Params
//   i2c: barramento I2C.  connection:mandatory.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err:   erro de inicialização.  connection:optional.
//   value: leitura do sensor.      connection:mandatory.
```

---

## 7. Como a IDE interpreta cada tag

| Tag                         | Efeito visual                            | Efeito de validação                                    |
|-----------------------------|------------------------------------------|--------------------------------------------------------|
| `range:min..max`            | Badge `[min..max]` no pino               | Erro se valor constante fora do intervalo              |
| `range_min:` / `range_max:` | Badge `[≥min]` ou `[≤max]`               | Idem, unilateral                                       |
| `unit:`                     | Badge cinza informativo                  | Filtra conexões incompatíveis (unit diferente = aviso) |
| `options:a\|b\|c`           | Dropdown no painel de inspeção           | Erro se valor fora da lista                            |
| `default:`                  | Valor pré-preenchido quando desconectado | Sem erro se desconectado                               |
| `nonil:`                    | Borda sólida, cor de alerta              | Erro se pino não conectado                             |
| `encoding:tristate`         | Seletor -1 / 0 / 1                       | Aceita apenas esses três valores                       |
| `encoding:bitmask`          | Editor de checkboxes                     | —                                                      |
| `connection:optional`       | Símbolo ◎ no pino                        | Sem erro se desconectado                               |
| `connection:mandatory`      | Símbolo ◉ no pino                        | Erro se pino não conectado                             |

---

## 8. Compatibilidade com `go doc`

O comando `go doc` renderiza os comentários como texto simples, ignorando as tags.
O resultado é legível e correto:

```
func (s *RGBLed) SetColor(r, g, b int) (err error)

    SetColor sets the RGB color of the LED.

    Params
      r: intensidade do canal vermelho.  range:0..255.  unit:rgb.  default:0.
      g: intensidade do canal verde.     range:0..255.  unit:rgb.  default:0.
      b: intensidade do canal azul.      range:0..255.  unit:rgb.  default:0.

    Returns
      err: erro de escrita no hardware.  connection:optional.
```

Um desenvolvedor sem acesso à IDE lê a documentação normalmente.
A IDE apenas extrai as tags para enriquecer a experiência visual.
