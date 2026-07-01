## Como funciona

O sistema de Comunicação Live conecta a IDE IoTMaker a hardware externo em tempo real. Os dados fluem em ambas as direções:

**Hardware → IDE**: sensores enviam leituras via webhook → os componentes atualizam instantaneamente.

**IDE → Hardware**: arraste o slider do gauge → o valor é publicado no Redis.

## Passos de configuração

1. **Defina o nome do projeto** e salve — o servidor gera um Project ID único.
2. **Crie uma API Key** — uma chave por projeto. A chave é exibida apenas uma vez.
3. **Clique em Conectar** — a IDE abre um WebSocket com o servidor.
4. **Envie dados do hardware** — use a URL do webhook com a chave de API.

## Enviando dados para a IDE

### Batch (recomendado) — todos os devices em uma requisição

```bash
curl -X POST http://localhost:8080/api/v1/webhook/{project_id} \
  -H "Content-Type: application/json" \
  -H "X-API-Key: {your_key}" \
  -d '[
    {"device_id":"gauge_1","port":"current","value":73},
    {"device_id":"gauge_2","port":"current","value":42},
    {"device_id":"gauge_3","port":"max","value":200}
  ]'
```

### Go — loop batch

```go
package main

import (
    "bytes"
    "fmt"
    "math/rand"
    "net/http"
    "time"
)

const (
    baseURL = "http://localhost:8080"
    project = "{project_id}"
    apiKey  = "{your_key}"
)

func sendBatch(items string) error {
    url := fmt.Sprintf("%s/api/v1/webhook/%s", baseURL, project)
    req, _ := http.NewRequest("POST", url, bytes.NewBufferString(items))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-API-Key", apiKey)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    return nil
}

func main() {
    for {
        batch := fmt.Sprintf(`[
            {"device_id":"gauge_1","port":"current","value":%d},
            {"device_id":"gauge_2","port":"current","value":%d}
        ]`, rand.Intn(100), rand.Intn(100))

        if err := sendBatch(batch); err != nil {
            fmt.Println("Erro:", err)
        } else {
            fmt.Println("Batch enviado")
        }
        time.Sleep(time.Second)
    }
}
```

## Lendo dados da IDE

Quando o usuário interage com um componente (ex: arrasta o slider), o valor é publicado no canal Redis `live:out:{user_id}:{project_id}`.

### Bash

```bash
redis-cli PSUBSCRIBE "live:out:*"
```

### Go

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/redis/go-redis/v9"
)

type LiveMessage struct {
    DeviceID string          `json:"device_id"`
    Port     string          `json:"port"`
    Value    json.RawMessage `json:"value"`
    Ts       int64           `json:"ts"`
}

func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    sub := rdb.PSubscribe(context.Background(), "live:out:*")
    defer sub.Close()

    fmt.Println("Ouvindo...")
    for msg := range sub.Channel() {
        var m LiveMessage
        json.Unmarshal([]byte(msg.Payload), &m)
        fmt.Printf("[%s.%s] = %s\n", m.DeviceID, m.Port, string(m.Value))
    }
}
```

## Portas suportadas

O **Gauge** suporta:

| Porta | Tipo | Descrição |
|-------|------|-----------|
| `current` | int | Posição da agulha |
| `max` | int | Máximo da escala |
| `min` | int | Mínimo da escala |

## Segurança

- Chaves são por projeto — uma chave controla todo o painel.
- Chaves não têm validade — hardware em campo não precisa trocar credenciais.
- Chaves podem ser revogadas instantaneamente neste painel.
- Apenas o hash SHA-256 é armazenado — a chave bruta é exibida uma única vez.
