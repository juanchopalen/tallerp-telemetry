```markdown
# TallERP Telemetry Server — Soporte GT02
## Extensión multiprotocolo sobre la implementación existente GT06

### Contexto

El repositorio actual es:

`github.com/juanchopalen/tallerp-telemetry`

Ya existe una implementación en producción que:

- escucha TCP en `:8899`;
- procesa trackers N01K;
- soporta GT06;
- valida frames y CRC;
- procesa login;
- procesa GPS;
- procesa heartbeat;
- responde ACK correctamente;
- genera eventos normalizados;
- persiste temporalmente en spool SQLite;
- entrega eventos por batch al API interno de TallERP/Laravel;
- maneja idempotencia, retries y delivery;
- está validado con hardware real en producción.

Ahora se requiere agregar soporte para trackers OBD que utilizan protocolo GT02.

IMPORTANTE:

NO crear un segundo servidor TCP.

NO crear otro spool.

NO crear otro delivery worker.

NO duplicar infraestructura existente.

GT02 y GT06 deben coexistir dentro del mismo servicio `tallerp-telemetry`.

---

# Fuente de verdad

Usar como fuente principal la documentación GT02 proporcionada por el fabricante docs/GT02.pdf:

`GPS locator Communication protocol`

NO asumir formatos encontrados en Internet.

NO usar definiciones genéricas de GT02/GT06 externas si contradicen esta documentación.

El protocolo documentado usa:

```text
78 78
```

para frames con longitud de 1 byte, y:

```text
79 79
```

para frames con longitud de 2 bytes.

Protocolos relevantes:

```text
0x01 Login
0x31 Location GPS/LBS UTC
0x32 Alarm
0x26 Alarm ACK
0x13 Heartbeat
0x34 LBS
0x33 WiFi
0x94 General information
0x8D Large file transfer
0x80 Server command
0x21 Command response
0x2A GPS/address query
```

---

# Objetivo

Agregar soporte GT02 al servidor actual de forma que:

```text
N01K / GT06
        \
         \
          > TCP :8899
         /
OBD / GT02
```

ambos terminen generando el mismo modelo normalizado de telemetría utilizado actualmente por Laravel.

Laravel NO debe necesitar lógica especial por modelo ni protocolo para procesar posiciones.

---

# Arquitectura requerida

Mantener la arquitectura existente:

```text
TCP Server
   ↓
Frame Decoder
   ↓
Protocol Detection
   ↓
GT06 Parser
   OR
GT02 Parser
   ↓
Normalized TelemetryEvent
   ↓
Durable Spool
   ↓
Batch Delivery
   ↓
Laravel
```

No alterar el contrato existente con Laravel salvo que sea estrictamente necesario.

---

# 1. Revisar implementación existente primero

Antes de modificar código:

1. revisar toda la implementación GT06 actual;
2. identificar:
   - framing;
   - CRC;
   - session context;
   - login;
   - ACK;
   - telemetry normalization;
   - spool;
   - delivery;
   - logging;
3. determinar qué código es realmente compartido;
4. reutilizarlo.

NO copiar archivos GT06 y renombrarlos GT02 si la lógica puede abstraerse.

Si la implementación actual mezcla lógica específica GT06 dentro del TCP server, refactorizar únicamente lo necesario para soportar múltiples protocolos.

---

# 2. Interfaz de protocolo

Crear o reutilizar una abstracción similar a:

```go
type ProtocolHandler interface {
    Name() string

    CanHandle(frame RawFrame, ctx *ConnectionContext) bool

    Handle(
        frame RawFrame,
        ctx *ConnectionContext,
    ) ([]TelemetryEvent, []byte, error)
}
```

La forma exacta puede ajustarse a la arquitectura existente.

Objetivo:

- GT06 implementa un handler;
- GT02 implementa otro;
- TCP server no contiene switchs extensos específicos de cada protocolo.

---

# 3. Detección de protocolo

NO asumir que el header diferencia GT02 de GT06.

Ambos pueden utilizar:

```text
78 78
79 79
```

y ambos pueden usar:

```text
0x01 login
0x13 heartbeat
```

La detección debe utilizar una combinación de:

- estructura del login;
- packet length;
- campos específicos;
- type identification code;
- modelo/protocolo ya conocido de una sesión;
- paquetes posteriores.

El contexto de conexión debe conservar:

```go
type ConnectionContext struct {
    IMEI       string
    Protocol   string
    DeviceType string
    ...
}
```

Una vez identificado el protocolo de la sesión, no redetectarlo en cada paquete salvo error.

---

# 4. Framing

Reutilizar el framing existente si ya soporta correctamente:

```text
78 78
79 79
```

En GT02:

- `78 78` → packet length de 1 byte;
- `79 79` → packet length de 2 bytes.

La documentación define:

```text
packet length =
protocol number
+ information content
+ serial number
+ CRC
```

El frame completo además contiene:

```text
start bits
length
protocol
content
serial
CRC
stop bits
```

Stop bits:

```text
0D 0A
```

Mantener soporte para:

- frame fragmentado entre múltiples TCP reads;
- varios frames en el mismo read;
- bytes basura antes de un frame;
- frames incompletos.

---

# 5. CRC

GT02 usa CRC-ITU.

La documentación proporciona el algoritmo exacto:

```text
initial value = 0xFFFF
table-based crc-itu
final value = bitwise NOT
```

El CRC se calcula desde:

```text
packet length
```

hasta:

```text
information serial number
```

incluyendo ambos.

NO reutilizar automáticamente el CRC de GT06 si la implementación actual usa una variante diferente.

Comparar contra los ejemplos del documento.

Crear tests con los vectores documentados.

---

# 6. Login GT02 — 0x01

El login GT02 incluye:

```text
Terminal ID / IMEI     8 bytes
Type identification    2 bytes
Timezone/language      2 bytes
Serial                 2 bytes
CRC                    2 bytes
```

El IMEI se codifica BCD con un `0` inicial para IMEI de 15 dígitos.

Ejemplo de la documentación:

```text
123456789012345
→
01 23 45 67 89 01 23 45
```

Crear o reutilizar:

```go
type GT02LoginPacket struct {
    IMEI       string
    DeviceType uint16
    Timezone   ...
    Language   ...
    Serial     uint16
}
```

Registrar:

```text
event=tracker_login
protocol=gt02
imei=...
device_type=...
```

---

# 7. ACK Login GT02

El servidor debe responder `0x01`.

Formato:

```text
78 78
05
01
SERIAL
CRC
0D 0A
```

El serial debe ser el mismo recibido.

La documentación indica que si el tracker no recibe respuesta dentro de aproximadamente 5 segundos, considera la conexión anormal.

Enviar ACK inmediatamente después de validar el login.

No esperar delivery a Laravel.

---

# 8. GPS / Location — 0x31

Implementar parser para:

```text
0x31
```

Contenido documentado:

```text
Date time UTC              6 bytes
GPS length/satellites      1 byte
Latitude                   4 bytes
Longitude                  4 bytes
Speed                      1 byte
Heading/status             2 bytes
MCC                        2 bytes
MNC                        2 bytes
LAC                        2 bytes
Cell ID                    4 bytes
ACC                        1 byte
Data reporting mode        1 byte
Realtime/supplement flag   1 byte
Serial                     2 bytes
CRC
```

---

# 9. Fecha GPS

Todos los tiempos del documento GT02 deben interpretarse como:

```text
UTC
```

No usar UTC+8.

No usar timezone del servidor.

Generar:

```text
gps_at
received_at
```

separadamente.

---

# 10. Latitud / longitud

La documentación establece:

```text
decimal_minutes × 30000
```

para codificación.

Decodificar correctamente.

Aplicar signos usando los bits East/West y North/South del campo heading/status.

Centralizar:

```go
func DecodeGT02Latitude(...)
func DecodeGT02Longitude(...)
```

Crear tests usando los ejemplos de la documentación.

---

# 11. Velocidad

El campo speed es:

```text
0x00 - 0xFF
→
0 - 255 km/h
```

Normalizar a:

```text
speed_kmh
```

---

# 12. Heading y status

Decodificar:

```text
GPS realtime/differential
GPS located
East/West
North/South
Heading 0..360
```

Mapear al TelemetryEvent normalizado.

No asumir que los mismos bits significan exactamente lo mismo que en GT06.

---

# 13. ACC GT02

GT02 `0x31` tiene un byte ACC explícito:

```text
00 = low
01 = high
```

Mapear a:

```text
acc
```

Pero NO introducir todavía lógica de negocio basada en ACC.

Solo transmitir el valor recibido.

---

# 14. Realtime / Supplementary transmission

Este campo es especialmente importante.

La documentación define:

```text
0x00 = realtime upload
0x01 = supplementary transmission
```

Agregar al modelo normalizado:

```go
IsRetransmission bool
```

o reutilizar campo equivalente existente.

Ejemplo:

```json
{
  "is_retransmission": true
}
```

Laravel debe poder conservarlo.

Si el contrato actual no lo soporta, agregarlo de manera backward-compatible.

---

# 15. Heartbeat GT02 — 0x13

Implementar parser según documentación.

Extraer:

```text
Terminal information
Voltage level
GSM signal strength
Language / extension status
Serial
```

Decodificar terminal information:

```text
Bit7 oil/electric state
Bit6 GPS located
Bits3-5 alarm state
Bit2 external power / charging
Bit1 ACC
Bit0 armed/disarmed
```

Normalizar únicamente campos que TallERP ya usa o tiene sentido conservar.

Como mínimo:

```text
acc
gps_located
external_power
voltage_level
gsm_signal
```

---

# 16. ACK Heartbeat GT02

Responder:

```text
78 78
05
13
SERIAL
CRC
0D 0A
```

con mismo serial.

Enviar inmediatamente.

No esperar spool/delivery.

---

# 17. Alarm GT02 — 0x32

Implementar parser.

Contiene:

- GPS;
- LBS;
- terminal status;
- voltage;
- GSM;
- alarm/language;
- serial.

Tipos documentados incluyen, entre otros:

```text
0x00 normal
0x01 SOS
0x02 power failure
0x03 vibration
0x04 enter fence
0x05 exit fence
0x06 overspeed
0x09 displacement
0x0A enter GPS blind area
0x0B leave GPS blind area
0x0C startup
0x0E low external power
0x11 shutdown
0x13 removal alarm
0x14 door alarm
0x15 low power shutdown
0x2C collision
0x2D rollover
0x2E sharp turn
0x28 rapid deceleration
0x29 rapid acceleration
```

Normalizar a:

```go
event_type = "alarm"
alarm_type = ...
```

sin inventar traducciones no documentadas.

---

# 18. ACK Alarm GT02

La documentación indica:

```text
Alarm received: 0x32
Server ACK:     0x26
```

Esto es importante.

NO responder `0x32`.

Construir:

```text
78 78
05
26
SERIAL
CRC
0D 0A
```

usando el serial correspondiente.

Agregar tests.

---

# 19. LBS — 0x34

Implementar parser básico para:

```text
0x34
```

Extraer:

```text
UTC datetime
TA
MCC
MNC
CellNum
LAC
Cell ID
RSSI
```

No es necesario convertir LBS a coordenadas todavía.

Generar un evento normalizado tipo:

```text
lbs
```

o preservar como evento técnico si Laravel no consume LBS aún.

No llamar servicios externos de geolocalización.

---

# 20. WiFi — 0x33

Implementar al menos parsing estructural:

```text
MCC
MNC
LAC
Cell ID
RSSI
WiFi count
WiFi MACs
WiFi signal strengths
```

No implementar geolocalización WiFi.

Generar evento técnico o conservar para futuras funciones.

---

# 21. General information — 0x94

Soportar framing y parsing del subprotocol.

La documentación define subtipos como:

```text
0x00 external voltage
0x04 terminal status sync
0x05 door status
0x08 self-test
0x09 satellite info
0x0A ICCID / IMSI / IMEI
```

Priorizar:

```text
0x0A
```

porque puede proporcionar:

```text
IMEI
IMSI
ICCID
```

No exponer IMSI/ICCID innecesariamente al frontend.

Registrar de forma segura.

---

# 22. Command response — 0x21

Implementar parser para respuestas del dispositivo.

La documentación define:

```text
79 79
length 2 bytes
protocol 0x21
server flag
encoding
content
serial
CRC
0D 0A
```

Contenido puede ser ASCII o UTF16-BE.

Registrar:

```text
event=command_response
imei=...
command_response=...
```

Preparar para uso futuro desde Laravel.

---

# 23. Server commands — 0x80

NO construir aún UI ni endpoint de comandos si no existe.

Pero el protocolo debe poder construir comandos GT02 `0x80`.

El contenido del comando es ASCII compatible con SMS commands.

Preparar función:

```go
BuildGT02Command(
    serverFlag uint32,
    command string,
    language ...
)
```

No ejecutar comandos automáticamente.

---

# 24. Comandos documentados

Mantener listado documentado para uso futuro:

```text
STATUS#
VERSION#
PARAM#
RESET#
SOS,...
SERVER,0,IP,port,0#
SERVER,1,domain,port,0#
POWERALM,...
BATALM,...
MOVING,...
RELAY,...
```

No agregar comandos no presentes en la documentación.

---

# 25. Cambio de servidor

La documentación GT02 confirma:

Por IP:

```text
SERVER,0,IP,port,0#
```

Ejemplo:

```text
SERVER,0,120.24.248.12,8005,0#
```

Por dominio:

```text
SERVER,1,domain,port,0#
```

Para TallERP:

```text
SERVER,1,telemetry.tallerp.com,8899,0#
```

No automatizar todavía esta operación.

Solo documentarla en:

```text
docs/gt02.md
```

---

# 26. Modelo normalizado

GT02 debe producir el mismo `TelemetryEvent` que GT06.

Agregar únicamente campos adicionales si son generales y útiles:

```go
type TelemetryEvent struct {
    EventID            string
    EventType          string
    Protocol           string
    IMEI               string

    GPSAt              *time.Time
    ReceivedAt         time.Time

    Latitude           *float64
    Longitude          *float64
    SpeedKmh           *float64
    Heading            *uint16
    Satellites         *uint8

    GPSLocated         *bool
    ACC                *bool
    ExternalPower      *bool
    GSMSignal          *uint8
    VoltageLevel       *uint8

    IsRetransmission   *bool

    AlarmType          *string

    Serial             uint16
    RawHex             string
}
```

Ajustar a la estructura real existente.

NO crear un TelemetryEventGT02 separado para delivery.

---

# 27. Laravel

El endpoint Laravel debe continuar recibiendo eventos en el mismo batch existente.

Solo ampliar payload si agregamos:

```text
protocol = gt02
is_retransmission
alarm_type
```

No cambiar endpoint.

No cambiar autenticación.

No cambiar spool architecture.

No cambiar delivery worker.

---

# 28. Event ID

Mantener estrategia idempotente existente.

Asegurar que GT02 retransmitido genere el mismo EventID si es exactamente el mismo paquete.

No incluir `received_at` si eso provoca IDs diferentes para la misma retransmisión.

Preferir datos estables como:

```text
imei
protocol
serial
gps_at
raw_hex
```

según la implementación existente.

---

# 29. Logging

Ejemplo:

```text
event=tracker_login
protocol=gt02
imei=...
device_type=...
```

Location:

```text
event=location
protocol=gt02
imei=...
gps_at=...
lat=...
lon=...
speed_kmh=...
is_retransmission=false
```

Alarm:

```text
event=alarm
protocol=gt02
alarm_type=overspeed
```

Mantener `raw_hex` en PoC/testing.

---

# 30. Métricas

Separar métricas por protocolo cuando sea útil:

```text
frames_received_gt06
frames_received_gt02

locations_received_gt06
locations_received_gt02

protocol_detection_failure
```

No romper las métricas agregadas existentes.

---

# 31. Tests de CRC

Agregar tests exactos usando ejemplos del documento.

Usar especialmente:

Login sample:

```text
78 78 11 01 08 66 54 50 52 50 16 96 28 01 32 01 00 01 69 04 0D 0A
```

Status sample:

```text
78 78 0A 13 00 05 04 00 01 00 0E 0E 4A 0D 0A
```

GPS sample:

```text
78 78 24 31 14 08 13 08 2D 04 CB 02 6C 6F 6C 0C 37 13 A6 00 14 00 01 CC 00 01 25 FC 06 14 64 02 00 00 00 00 0B CD 21 0D 0A
```

---

# 32. Tests Login

Validar:

- IMEI;
- device type;
- timezone/language;
- serial;
- CRC;
- ACK.

---

# 33. Tests GPS 0x31

Validar:

- UTC datetime;
- satellites;
- latitude;
- longitude;
- speed;
- heading;
- GPS located;
- East/West;
- North/South;
- ACC;
- supplementary transmission;
- serial.

---

# 34. Tests Heartbeat

Validar:

- status bits;
- voltage;
- GSM;
- external power;
- ACC;
- GPS located;
- ACK serial.

---

# 35. Tests Alarm

Validar:

- parser `0x32`;
- alarm type;
- GPS;
- status;
- ACK `0x26`.

---

# 36. Tests 0x94

Validar al menos:

```text
subtype 0x0A
```

para:

- IMEI;
- IMSI;
- ICCID.

---

# 37. Tests multiprotocolo

Crear tests donde el mismo servidor procese:

```text
GT06 tracker A
GT02 tracker B
```

simultáneamente.

Validar que:

- cada conexión conserva su protocolo;
- no se mezclan parsers;
- ambos generan eventos normalizados;
- ambos llegan al mismo spool;
- ambos se entregan en batch.

---

# 38. Comportamiento ante protocolo ambiguo

Si no puede determinarse protocolo:

```text
event=protocol_detection_failed
raw_hex=...
```

No hacer panic.

No cerrar inmediatamente si es posible esperar otro frame para identificar la sesión.

Aplicar un límite razonable de frames ambiguos.

---

# 39. Backward compatibility

La implementación GT06 existente debe continuar funcionando sin cambios funcionales.

Todos los tests existentes deben pasar.

No modificar comportamiento del N01K salvo refactor estrictamente necesario.

---

# 40. Documentación

Crear:

```text
docs/gt02.md
```

Incluir:

- frame format;
- CRC;
- login;
- GPS `0x31`;
- heartbeat;
- alarm;
- supplementary upload;
- commands;
- cambio de server;
- limitaciones conocidas.

También actualizar README:

```text
Supported protocols:
- GT06
- GT02
```

---

# 41. No implementar todavía

NO implementar:

- cálculo de kilometraje especial por GT02;
- mapas;
- geocercas;
- comandos desde UI;
- control de relay;
- SMS;
- almacenamiento histórico de WiFi/LBS si no es necesario;
- cambio automático de server;
- modelo específico de OBD en Laravel.

---

# 42. Criterios de aceptación

La tarea queda completa cuando:

1. GT06 sigue funcionando.
2. GT02 es soportado por el mismo puerto TCP.
3. GT02 login `0x01` funciona.
4. ACK login GT02 es correcto.
5. GPS GT02 `0x31` se decodifica.
6. `supplementary transmission` se detecta.
7. Heartbeat `0x13` funciona.
8. ACK heartbeat es correcto.
9. Alarm `0x32` funciona.
10. ACK alarm usa `0x26`.
11. LBS `0x34` se reconoce.
12. WiFi `0x33` se reconoce.
13. General `0x94` se reconoce.
14. Command response `0x21` se reconoce.
15. GT02 genera el mismo TelemetryEvent normalizado.
16. Spool y batch existentes siguen funcionando.
17. GT02 y GT06 pueden coexistir simultáneamente.
18. Todos los tests existentes siguen pasando.
19. Nuevos tests GT02 pasan.
20. `go vet ./...` pasa.
21. `go test ./...` pasa.
22. build Linux ARM64 pasa.

---

# 43. Entregable

Antes de desplegar:

Mostrar:

1. archivos creados;
2. archivos modificados;
3. refactor realizado;
4. estrategia de detección de protocolo;
5. diferencias GT02 vs GT06;
6. ACKs implementados;
7. soporte de retransmisión;
8. resultado de tests;
9. build ARM64;
10. cualquier punto que requiera validación con hardware real.

NO desplegar automáticamente.

La validación final se hará cuando lleguen las muestras OBD reales.
```

La parte crítica de la implementación es no tratar GT02 como “otro GT06 con números distintos”. El documento muestra diferencias reales, especialmente GPS `0x31`, alarmas `0x32 → ACK 0x26` y el flag explícito de retransmisión histórica. 