```markdown
# TallERP Telemetry Server — Fase 2
## Persistencia durable e integración con TallERP Laravel

La Fase 1 ya está implementada y validada con hardware N01K real.

Actualmente funciona correctamente:

- servidor TCP público en `:8899`
- conexiones concurrentes
- login `0x01`
- extracción real de IMEI
- ACK de login
- GPS `0x12`
- heartbeat `0x13`
- ACK de heartbeat
- logging estructurado
- separación `gps_at` / `received_at`

Prueba real confirmada:

IMEI:

867111066918621

El tracker se conecta directamente a TallERP sin pasar por Smake.

Ejemplo real recibido:

```text
event=location
imei=867111066918621
gps_at=2026-08-28T16:37:42Z
lat=10.494576111111112
lon=-66.85911666666667
speed_kmh=0
satellites=15
```

## Objetivo

La Fase 2 debe hacer que ningún dato recibido correctamente desde un tracker se pierda aunque:

- TallERP Laravel esté temporalmente caído;
- exista timeout HTTP;
- haya problemas de Internet entre Telemetry y Laravel;
- Laravel devuelva 500;
- el servidor se reinicie;
- lleguen posiciones duplicadas;
- lleguen posiciones atrasadas;
- el tracker retransmita información histórica.

NO implementar todavía cálculo de kilometraje.

NO implementar mapas.

NO implementar geocercas.

NO implementar mantenimiento recurrente.

NO implementar PostgreSQL.

---

## Arquitectura

Implementar:

```text
Tracker
   ↓
TCP Server Go
   ↓
Parser
   ↓
Telemetry Event
   ↓
Durable Spool
   ↓
Delivery Worker
   ↓ HTTPS
Laravel Internal Telemetry API
   ↓
MySQL
```

La recepción TCP nunca debe depender directamente de que Laravel esté disponible.

---

## 1. Crear modelo interno de evento

Crear una estructura común:

```go
type TelemetryEvent struct {
    EventID      string    `json:"event_id"`
    EventType    string    `json:"event_type"`

    IMEI         string    `json:"imei"`
    Protocol     string    `json:"protocol"`
    Serial       uint16    `json:"serial"`

    GPSAt        *time.Time `json:"gps_at,omitempty"`
    ReceivedAt   time.Time  `json:"received_at"`

    Latitude     *float64   `json:"latitude,omitempty"`
    Longitude    *float64   `json:"longitude,omitempty"`
    SpeedKmh     *float64   `json:"speed_kmh,omitempty"`
    Heading      *uint16    `json:"heading,omitempty"`
    Satellites   *uint8     `json:"satellites,omitempty"`

    GPSLocated   *bool      `json:"gps_located,omitempty"`
    RealtimeGPS  *bool      `json:"realtime_gps,omitempty"`
    ACC          *bool      `json:"acc,omitempty"`

    GSMSignal    *uint8     `json:"gsm_signal,omitempty"`
    VoltageLevel *uint8     `json:"voltage_level,omitempty"`

    RawHex       string     `json:"raw_hex"`
}
```

Ajustar tipos según las estructuras reales existentes.

No duplicar DTOs si ya existen estructuras equivalentes.

---

## 2. Event ID idempotente

Cada evento debe tener un identificador estable.

NO utilizar un UUID aleatorio como única protección contra duplicados.

Construir `event_id` de forma determinística utilizando al menos:

```text
IMEI
protocol
serial
gps_at cuando exista
raw payload
```

Preferir SHA-256.

Ejemplo conceptual:

```text
sha256(
  imei + protocol + serial + gps_at + raw_hex
)
```

De esta forma si el tracker retransmite exactamente el mismo paquete, TallERP podrá identificarlo como duplicado.

---

## 3. Durable spool

Antes de considerar procesado un evento, debe quedar persistido localmente.

Utilizar SQLite embebido.

NO instalar un servidor de base de datos adicional.

Archivo:

```text
/var/lib/tallerp-telemetry/spool.db
```

Usar SQLite únicamente como cola durable del servicio.

NO convertir SQLite en la base de datos principal de telemetría.

Crear conceptualmente:

```sql
telemetry_spool

id
event_id UNIQUE
payload_json
status
attempts
next_attempt_at
created_at
delivered_at
last_error
```

Estados:

```text
pending
delivering
delivered
failed
```

Preferir mantener únicamente:

```text
pending
delivered
```

si puede resolverse de forma más simple.

---

## 4. Flujo de recepción

Cuando llega un paquete válido:

```text
TCP
 ↓
validación
 ↓
parse
 ↓
ACK cuando corresponda
 ↓
crear TelemetryEvent
 ↓
persistir en spool
```

IMPORTANTE:

Los ACK del protocolo NO deben esperar a que Laravel responda.

Por ejemplo:

```text
heartbeat recibido
        ↓
validar frame
        ↓
responder ACK inmediatamente
        ↓
crear evento
        ↓
spool
```

La conexión con el tracker debe mantenerse independiente del backend TallERP.

---

## 5. Delivery Worker

Crear un worker interno encargado de enviar eventos pendientes a Laravel.

Configurable:

```env
TALLERP_API_URL=https://tallerp.com
TALLERP_TELEMETRY_TOKEN=
TALLERP_DELIVERY_BATCH_SIZE=100
TALLERP_DELIVERY_INTERVAL_SECONDS=5
TALLERP_HTTP_TIMEOUT_SECONDS=10
```

NO utilizar `env` disperso.

Crear estructura central de configuración.

---

## 6. Endpoint Laravel esperado

El servicio enviará eventos a:

```text
POST /api/internal/telemetry/events
```

Payload recomendado:

```json
{
  "events": [
    {
      "event_id": "...",
      "event_type": "location",
      "imei": "867111066918621",
      "protocol": "0x12",
      "serial": 14,
      "gps_at": "2026-08-28T16:37:42Z",
      "received_at": "2026-08-28T16:37:44Z",
      "latitude": 10.494576111111112,
      "longitude": -66.85911666666667,
      "speed_kmh": 0,
      "heading": 0,
      "satellites": 15,
      "gps_located": true,
      "realtime_gps": true,
      "raw_hex": "..."
    }
  ]
}
```

No enviar un request por cada posición si puede enviarse por lotes.

---

## 7. Autenticación entre servidores

Utilizar token dedicado.

Header:

```text
Authorization: Bearer <token>
```

Agregar adicionalmente:

```text
User-Agent: TallERP-Telemetry/<version>
```

NO reutilizar:

- credenciales de usuarios;
- tokens de workshops;
- API keys de Smake;
- sesiones Laravel.

Crear una credencial exclusiva servidor-servidor.

---

## 8. Reintentos

Si Laravel responde:

```text
2xx
```

marcar eventos como entregados.

Si responde:

```text
408
429
500
502
503
504
```

mantener pendientes.

Aplicar exponential backoff.

Ejemplo conceptual:

```text
5 s
15 s
30 s
1 min
5 min
15 min
30 min
```

Agregar jitter.

No hacer loops agresivos.

---

## 9. Respuesta parcial

Laravel debe poder responder algo equivalente a:

```json
{
  "accepted": [
    "event_id_1",
    "event_id_2"
  ],
  "duplicates": [
    "event_id_3"
  ],
  "rejected": []
}
```

Tanto `accepted` como `duplicates` se consideran entregados correctamente.

Un duplicado NO es un error.

---

## 10. Orden temporal

NO asumir que los eventos llegan ordenados.

Ejemplo:

```text
received_at: 28 agosto
gps_at:      27 agosto
```

Esto puede ocurrir porque el tracker retransmite historial.

Laravel debe guardar:

```text
gps_at
received_at
```

por separado.

Nunca reemplazar `gps_at` por `received_at`.

---

## 11. ACC

En el N01K actualmente probado, el cable ACC no está conectado físicamente.

Por lo tanto:

- conservar el valor recibido;
- no interpretar `acc=true/false` como estado real del vehículo;
- no filtrar posiciones usando ACC;
- no calcular horómetro;
- no descartar kilometraje usando ACC.

Esta lógica se revisará posteriormente después de una prueba física con ACC conectado.

---

## 12. Heartbeats

No es necesario almacenar permanentemente cada heartbeat en MySQL.

Sin embargo, deben servir para actualizar:

```text
last_seen_at
last_gsm_signal
last_voltage_level
last_protocol
```

del tracker.

Go puede enviar eventos heartbeat y Laravel decidir cómo consolidarlos.

---

## 13. Location events

Las posiciones GPS sí deben almacenarse.

Tabla conceptual en Laravel:

```text
tracker_positions
```

Campos:

```text
id
event_id
vehicle_tracker_id
imei

gps_at
received_at

latitude
longitude
speed_kmh
heading
satellites

gps_located
realtime_gps
acc

protocol
serial
raw_hex

created_at
```

Agregar:

```text
UNIQUE(event_id)
```

Índices:

```text
vehicle_tracker_id + gps_at
imei + gps_at
gps_at
```

NO agregar todavía columnas de kilometraje.

---

## 14. Asociación del IMEI

Laravel debe resolver:

```text
IMEI
 ↓
vehicle_tracker
 ↓
vehicle
 ↓
workshop
```

Go NO necesita conocer:

```text
workshop_id
vehicle_id
customer_id
```

La resolución pertenece a Laravel.

Si llega un IMEI desconocido:

NO descartar la telemetría.

Registrar el evento como dispositivo no asociado o conservarlo en una tabla apropiada para revisión administrativa.

No permitir que un IMEI desconocido genere acceso a datos de ningún workshop.

---

## 15. Multi-tenancy

Toda consulta posterior desde TallERP debe respetar:

```text
workshop_id
```

Pero `tracker_positions` debe quedar asociado mediante `vehicle_tracker_id`.

Nunca confiar en `workshop_id` enviado desde Go.

Go no debe enviar `workshop_id`.

Laravel deriva el tenant a partir del tracker registrado.

---

## 16. Política de raw_hex

Durante la fase inicial guardar:

```text
raw_hex
```

Esto nos permitirá:

- corregir parser;
- investigar anomalías;
- validar retransmisiones;
- comparar firmware.

Posteriormente podremos establecer retención.

Por ahora no eliminarlo.

---

## 17. Limpieza del spool

Los eventos entregados no deben permanecer indefinidamente en SQLite.

Agregar limpieza periódica.

Configuración:

```text
TALLERP_SPOOL_RETENTION_HOURS=24
```

Los eventos `delivered` mayores al período pueden eliminarse.

Los `pending` NUNCA deben eliminarse automáticamente por antigüedad.

---

## 18. Backpressure

Si Laravel permanece caído varios días, el spool puede crecer.

Implementar métricas/logs:

```text
spool_pending_events
oldest_pending_event_age
spool_database_size
```

Agregar warning cuando:

```text
pending > 10000
```

y critical configurable cuando:

```text
pending > 100000
```

No detener la recepción TCP por esto salvo riesgo real de disco lleno.

---

## 19. Disk safety

Antes de escribir:

- manejar errores SQLite;
- manejar `disk full`;
- registrar error crítico.

Si no puede persistirse un evento:

registrar:

```text
event=telemetry_persistence_failure
```

con severidad ERROR/CRITICAL.

NO fingir que el evento fue procesado.

---

## 20. Graceful shutdown

Capturar:

```text
SIGTERM
SIGINT
```

Al apagar:

1. dejar de aceptar nuevas conexiones;
2. cerrar conexiones existentes razonablemente;
3. terminar batch en curso;
4. cerrar SQLite;
5. terminar proceso.

Compatible con systemd.

---

## 21. Health

Agregar servidor HTTP local independiente del TCP.

Configuración:

```text
TALLERP_HEALTH_PORT=8080
```

Endpoints:

```text
GET /health
GET /ready
```

`/health`:

proceso vivo.

`/ready`:

- listener TCP funcionando;
- SQLite disponible;
- spool writable.

NO hacer que `/ready` dependa de Laravel.

Laravel puede estar caído y Telemetry debe seguir recibiendo posiciones.

---

## 22. Métricas básicas

Sin introducir todavía Prometheus si no es necesario.

Logs estructurados para:

```text
connections_active
frames_received
locations_received
heartbeats_received
invalid_crc
unsupported_protocol
spool_pending
delivery_success
delivery_failure
```

Preparar código para métricas futuras.

---

## 23. Tests

Agregar tests para:

- insertar evento en spool;
- event_id idempotente;
- duplicate event;
- recuperar pendientes;
- marcar delivered;
- reintento;
- exponential backoff;
- Laravel 200;
- Laravel 500;
- Laravel timeout;
- Laravel 429;
- respuesta duplicate;
- batch parcial;
- reinicio del proceso con eventos pendientes;
- limpieza de delivered;
- pending no eliminado;
- graceful shutdown.

Mantener todos los tests de Fase 1.

Ejecutar:

```bash
go test ./...
go vet ./...
```

---

## 24. No cálculo de kilometraje

IMPORTANTE:

NO calcular:

```text
distance
daily mileage
odometer
```

en esta fase.

Primero necesitamos acumular telemetría real durante varios días.

El kilometraje se implementará posteriormente en Laravel utilizando:

```text
tracker_positions
```

---

## 25. Laravel

La implementación del endpoint Laravel pertenece al repositorio TallERP.

NO modificar el repositorio Laravel desde `tallerp-telemetry`.

Al finalizar esta tarea, documentar claramente el contrato HTTP requerido para que posteriormente Codex pueda implementarlo en TallERP.

Crear:

```text
docs/laravel-ingestion-api.md
```

con:

- endpoint;
- authentication;
- request;
- response;
- errores;
- idempotencia.

---

## 26. Configuración de producción

Crear `.env.example`:

```env
TALLERP_TELEMETRY_PORT=8899

TALLERP_SPOOL_PATH=/var/lib/tallerp-telemetry/spool.db
TALLERP_SPOOL_RETENTION_HOURS=24

TALLERP_API_URL=https://tallerp.com
TALLERP_TELEMETRY_TOKEN=

TALLERP_DELIVERY_BATCH_SIZE=100
TALLERP_DELIVERY_INTERVAL_SECONDS=5
TALLERP_HTTP_TIMEOUT_SECONDS=10

TALLERP_HEALTH_PORT=8080
```

No incluir secretos reales.

---

## 27. systemd

Actualizar documentación para que:

```text
/var/lib/tallerp-telemetry
```

pertenezca al usuario:

```text
tallerp-telemetry
```

El servicio debe tener permiso para crear:

```text
spool.db
spool.db-wal
spool.db-shm
```

Si SQLite utiliza WAL.

---

## 28. SQLite

Utilizar WAL mode si la librería elegida lo soporta correctamente:

```text
journal_mode=WAL
```

Mantener configuración simple.

No introducir ORM.

Utilizar SQL explícito y repositorio pequeño.

Preferir una dependencia SQLite estable y compatible con:

```text
linux/arm64
```

IMPORTANTE:

Evitar una dependencia que complique innecesariamente el cross-compilation ARM64.

Evaluar cuidadosamente driver SQLite pure-Go vs CGO.

Preferir opción que permita:

```bash
GOOS=linux GOARCH=arm64 go build
```

desde macOS sin toolchain C adicional.

---

## 29. Versionado

Agregar versión del servicio al binario.

Ejemplo:

```text
tallerp-telemetry version
```

o log al iniciar:

```text
version=0.2.0
```

Esta fase corresponde conceptualmente a:

```text
v0.2.0
```

---

## 30. Entregable

Al finalizar mostrar:

1. archivos creados;
2. archivos modificados;
3. esquema SQLite;
4. estrategia de `event_id`;
5. estrategia de retry;
6. contrato HTTP Laravel;
7. configuración nueva;
8. resultados de tests;
9. build Linux ARM64;
10. instrucciones de despliegue.

NO desplegar automáticamente.

Primero revisaré el código antes de actualizar la EC2.
```