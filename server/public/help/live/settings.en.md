## How it works

The Live Communication system connects the IoTMaker IDE to external hardware in real time. Data flows in both directions:

**Hardware → IDE**: sensors send readings via webhook → the components update instantly.

**IDE → Hardware**: drag a slider on the gauge → the value is published to Redis.

## Setup steps

1. **Set a Project Name** and save it — the server generates a unique Project ID.
2. **Create an API Key** — one key per project. The raw key is shown only once.
3. **Click Connect** — the IDE opens a WebSocket to the server.
4. **Send data from hardware** — use the webhook URL with the API key.

## Sending data to the IDE

### Batch (recommended) — all devices in one request

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

### Go — batch loop

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
            fmt.Println("Error:", err)
        } else {
            fmt.Println("Sent batch")
        }
        time.Sleep(time.Second)
    }
}
```

## Reading data from the IDE

When the user interacts with a component (e.g. drags a slider), the value is published to Redis channel `live:out:{user_id}:{project_id}`.

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

    fmt.Println("Listening...")
    for msg := range sub.Channel() {
        var m LiveMessage
        json.Unmarshal([]byte(msg.Payload), &m)
        fmt.Printf("[%s.%s] = %s\n", m.DeviceID, m.Port, string(m.Value))
    }
}
```

## Supported ports

The **Gauge** supports:

| Port | Type | Description |
|------|------|-------------|
| `current` | int | Needle position |
| `max` | int | Scale maximum |
| `min` | int | Scale minimum |

## Security

- Keys are scoped to one project — one key controls the entire dashboard.
- Keys have no expiration — hardware in the field doesn't need credential rotation.
- Keys can be revoked instantly from this panel.
- Only the SHA-256 hash is stored — the raw key is shown once at creation.
