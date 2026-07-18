# blink_led

Blinks forever. One input:

| pin | type | meaning |
|-----|------|---------|
| **interval_ms** | int | milliseconds between ON and OFF |

Small numbers blink fast (100 is frantic); big numbers blink slow
(2000 is a lighthouse). The block never returns — it IS the program's
heartbeat.
