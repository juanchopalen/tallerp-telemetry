# Contrato de ingestión de telemetría en Laravel

## Endpoint y autenticación

El servicio de telemetría llama a:

```http
POST /api/internal/telemetry/events
Authorization: Bearer <TALLERP_TELEMETRY_TOKEN>
Content-Type: application/json
Accept: application/json
User-Agent: TallERP-Telemetry/2
```

El token es una credencial exclusiva servidor-servidor. Laravel debe guardarlo
como secreto, compararlo de forma segura y no reutilizar sesiones, credenciales
de usuarios, tokens de workshops ni claves de Smake. En producción el endpoint
debe exponerse únicamente por HTTPS.

## Request

El cuerpo contiene un lote de hasta `TALLERP_DELIVERY_BATCH_SIZE` eventos:

```json
{
  "events": [
    {
      "event_id": "0ad0...",
      "event_type": "location",
      "imei": "867111066918621",
      "protocol": "0x12",
      "protocol_family": "gt06",
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
      "acc": false,
      "raw_hex": "7878...0d0a"
    }
  ]
}
```

Tipos de `event_type`: `login`, `location`, `heartbeat`, `alarm`, `lbs`,
`wifi`, `general_info` y `command_response`.
Los campos opcionales se omiten cuando el protocolo no los entrega. Laravel
debe mantener `gps_at` y `received_at` separados y aceptar eventos históricos o
fuera de orden. `acc` se conserva, pero por ahora no debe usarse para filtrar
posiciones ni inferir el estado real del vehículo.

`protocol` continúa siendo el número de mensaje para mantener compatibilidad e
idempotencia GT06. `protocol_family` es aditivo y vale `gt06` o `gt02`. GT02
puede añadir los siguientes campos opcionales:

- `external_power`, `is_retransmission` y `alarm_type`;
- `battery_percent`, `external_voltage`, `mcc`, `mnc` y `timing_advance`;
- `cells`: observaciones `{lac, cell_id, rssi}`;
- `wifi_access_points`: observaciones `{mac, signal_strength}`;
- `general_info`: subtipo y, para `0x0A`, IMEI/IMSI/ICCID;
- `command_response`: `server_flag`, `encoding` y `content`.

IMSI e ICCID son datos internos sensibles: Laravel puede conservarlos para
operación del tracker, pero no debe incluirlos en recursos ni respuestas del
frontend. LBS y WiFi no representan coordenadas calculadas.

Go nunca envía `workshop_id`, `vehicle_id` ni `customer_id`. Laravel resuelve el
IMEI mediante `vehicle_tracker` y de allí deriva vehículo y tenant. Un IMEI no
asociado debe conservarse para revisión sin darle acceso a datos de un workshop.

## Respuesta

Una respuesta exitosa usa HTTP 2xx y clasifica cada ID recibido:

```json
{
  "accepted": ["event_id_1"],
  "duplicates": ["event_id_2"],
  "rejected": ["event_id_3"]
}
```

`accepted` y `duplicates` son resultados definitivos y el spool los marca como
entregados. Un ID `rejected`, o ausente de las tres listas, permanece pendiente
y se reintenta. Laravel debería incluir cada ID exactamente una vez y, si
rechaza uno, registrar internamente una razón apta para diagnóstico (sin incluir
secretos). El servicio limita la respuesta leída a 1 MiB.

## Idempotencia y persistencia

`event_id` es un SHA-256 determinístico de IMEI, protocolo, serial, fecha GPS
cuando existe y paquete crudo. Para GT02 también incorpora la familia; la
fórmula histórica GT06 no cambia. Laravel debe imponer `UNIQUE(event_id)` y tratar
una colisión con una fila ya insertada como `duplicates`, no como error.

Para `location`, la inserción de la posición y la clasificación del ID deben ser
atómicas. Índices recomendados para `tracker_positions`:

- `UNIQUE(event_id)`;
- `(vehicle_tracker_id, gps_at)`;
- `(imei, gps_at)`;
- `(gps_at)`.

Los heartbeat pueden consolidarse en `last_seen_at`, `last_gsm_signal`,
`last_voltage_level` y `last_protocol`; no es obligatorio guardar cada uno de
forma permanente. No se calcula kilometraje, distancia ni odómetro en esta fase.

## Errores y reintentos

- `401`/`403`: token ausente o inválido; requiere intervención operativa.
- `408`, `429`, `500`, `502`, `503`, `504`: fallo temporal; el lote queda en el
  spool y se reintenta con backoff exponencial y jitter.
- Cualquier respuesta no 2xx, timeout, error de red, JSON inválido o respuesta
  2xx que no acepte un ID deja ese ID pendiente.

Laravel puede usar `429` y `Retry-After` para indicar saturación, aunque esta
versión del worker aplica su propia secuencia de backoff.
