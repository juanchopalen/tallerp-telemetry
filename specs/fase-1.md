```markdown
# TallERP Telemetry Server — Fase 1
## Servidor TCP directo para trackers GPS

### Contexto

Estamos creando un nuevo servicio independiente llamado:

`tallerp-telemetry`

Repositorio:

`github.com/juanchopalen/tallerp-telemetry`

Lenguaje:

Go 1.26+

Arquitectura de despliegue inicial:

- AWS EC2 t4g.nano
- Ubuntu ARM64
- TCP público
- Puerto inicial: `8899`

Actualmente ya existe una prueba mínima en la EC2 que confirma que:

- el puerto TCP 8899 está accesible desde Internet;
- Go puede escuchar conexiones externas correctamente.

El objetivo ahora es reemplazar esa prueba temporal por una implementación formal y testeada del protocolo GPS documentado.

---

# Fuente de verdad

Antes de implementar, revisar toda la documentación del protocolo incluida en el proyecto.

El documento principal es:

`GPS Locator communication protocol`

NO asumir formatos de otros protocolos GT06 encontrados en Internet.

NO inventar campos.

NO inferir ACKs que no estén documentados.

Si existe una contradicción entre conocimiento externo y el PDF suministrado, usar el PDF como fuente de verdad.

---

# Alcance de esta fase

Implementar únicamente:

1. servidor TCP;
2. framing de paquetes;
3. validación del protocolo;
4. CRC;
5. login;
6. GPS/LBS;
7. heartbeat;
8. alarmas;
9. ACKs requeridos;
10. logging estructurado;
11. pruebas automatizadas.

NO implementar todavía:

- MySQL;
- PostgreSQL;
- Laravel;
- API HTTP;
- cálculo de kilometraje;
- mantenimiento preventivo;
- asociación con talleres;
- asociación con vehículos;
- Smake API;
- geocercas;
- dashboard;
- comandos remotos desde TallERP.

Esta fase debe demostrar que un tracker real puede conectarse directamente al servidor de TallERP y mantener una sesión estable.

---

# 1. Estructura del proyecto

Utilizar una estructura simple y mantenible:

```text
tallerp-telemetry/
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── server/
│   │   ├── tcp_server.go
│   │   └── connection.go
│   │
│   ├── protocol/
│   │   ├── frame.go
│   │   ├── decoder.go
│   │   ├── encoder.go
│   │   ├── crc.go
│   │   ├── login.go
│   │   ├── location.go
│   │   ├── heartbeat.go
│   │   └── alarm.go
│   │
│   └── telemetry/
│       └── logger.go
│
├── go.mod
├── README.md
└── .gitignore
```

No crear arquitectura innecesariamente compleja.

---

# 2. Configuración

El servidor debe aceptar:

```text
TALLERP_TELEMETRY_PORT
```

Valor predeterminado:

```text
8899
```

Ejemplo:

```bash
TALLERP_TELEMETRY_PORT=8899 go run ./cmd/server
```

Escuchar:

```text
0.0.0.0:8899
```

---

# 3. Servidor TCP

Implementar un servidor concurrente usando `net.Listen`.

Cada conexión debe ejecutarse en su propia goroutine.

Registrar:

- IP remota;
- puerto remoto;
- fecha/hora UTC de conexión;
- fecha/hora UTC de desconexión;
- cantidad de bytes recibidos;
- errores de socket.

No bloquear el listener mientras se procesa un dispositivo.

Aplicar timeouts razonables mediante:

```go
SetReadDeadline
SetWriteDeadline
```

pero sin cerrar conexiones saludables de trackers que permanezcan conectados.

---

# 4. Framing del protocolo

Según la documentación, los paquetes normales comienzan con:

```hex
78 78
```

Algunos tipos especiales pueden comenzar con:

```hex
79 79
```

Soportar ambos formatos arquitectónicamente, aunque inicialmente se implementen principalmente los `0x78 0x78`.

Los paquetes normales contienen:

```text
Starting bits          2 bytes
Packet length          1 byte
Protocol number        1 byte
Information content    N bytes
Message serial         2 bytes
CRC                    2 bytes
Stop bits              2 bytes
```

Stop bits:

```hex
0D 0A
```

El decoder NO debe asumir que un `Read()` equivale a un paquete completo.

TCP puede entregar:

- medio paquete;
- un paquete;
- varios paquetes concatenados.

Implementar buffering por conexión.

El decoder debe:

1. encontrar cabecera válida;
2. determinar longitud;
3. esperar hasta disponer de todos los bytes;
4. extraer exactamente una trama;
5. conservar bytes restantes;
6. continuar procesando.

---

# 5. CRC

El protocolo utiliza CRC-ITU.

La documentación indica que el CRC se calcula desde:

```text
Packet Length
```

hasta:

```text
Message Serial Number
```

incluyendo ambos.

Si el CRC no coincide:

- NO procesar el paquete;
- NO responder ACK;
- registrar el error;
- registrar raw HEX;
- continuar procesando la conexión.

Implementar:

```go
func CalculateCRC(data []byte) uint16
```

No utilizar una implementación arbitraria.

Comparar contra los ejemplos del PDF para validar exactamente la variante CRC usada.

---

# 6. Objeto Frame

Crear una representación similar a:

```go
type Frame struct {
    Header         []byte
    Length         int
    ProtocolNumber byte
    Content        []byte
    Serial         uint16
    CRC            uint16
    Raw            []byte
}
```

El parser genérico debe devolver primero un `Frame`.

Luego cada protocolo interpreta `Content`.

---

# 7. Login — protocolo 0x01

Implementar soporte completo del paquete:

```text
Protocol Number: 0x01
```

El contenido incluye el Terminal ID / IMEI.

La documentación indica un IMEI de 15 dígitos representado en 8 bytes.

Crear:

```go
type LoginPacket struct {
    IMEI   string
    Serial uint16
}
```

Validar:

- IMEI correctamente decodificado;
- longitud;
- CRC;
- stop bits.

Registrar:

```text
event=tracker_login
imei=867111066918647
remote_ip=...
serial=...
```

---

# 8. ACK de Login

El login requiere respuesta del servidor.

Formato:

```text
78 78
05
01
SERIAL_HIGH SERIAL_LOW
CRC_HIGH CRC_LOW
0D 0A
```

El protocol number de respuesta debe ser:

```hex
01
```

y debe utilizar el MISMO serial del paquete recibido.

La documentación proporciona como ejemplo:

Entrada:

```hex
78 78 0D 01
01 23 45 67 89 01 23 45
00 01
8C DD
0D 0A
```

Respuesta:

```hex
78 78 05 01
00 01
D9 DC
0D 0A
```

Crear:

```go
func BuildLoginACK(serial uint16) ([]byte, error)
```

Agregar test exacto utilizando este vector.

El tracker considera anormal la conexión si no recibe respuesta de login dentro de aproximadamente 5 segundos.

Por lo tanto:

**el ACK debe enviarse inmediatamente después de validar el login.**

---

# 9. Mantener identidad por conexión

Después de login:

```go
type ConnectionContext struct {
    IMEI      string
    RemoteIP  string
    ConnectedAt time.Time
}
```

Todos los paquetes posteriores recibidos por esa conexión deben quedar asociados al IMEI identificado durante login.

Si llega GPS antes de login:

- registrar warning;
- no asumir IMEI;
- continuar sin panicar.

---

# 10. GPS/LBS — protocolo 0x12

Implementar:

```text
Protocol Number: 0x12
```

Este paquete contiene:

- fecha/hora GPS;
- cantidad de satélites;
- latitud;
- longitud;
- velocidad;
- heading;
- estado GPS;
- ACC cuando aplique;
- MCC;
- MNC;
- LAC;
- Cell ID;
- serial.

Crear:

```go
type LocationPacket struct {
    IMEI       string
    GPSAt      time.Time
    Latitude   float64
    Longitude  float64
    SpeedKmh   uint8
    Heading    uint16
    Satellites uint8

    GPSLocated bool
    RealtimeGPS bool

    ACC *bool

    MCC uint16
    MNC uint8
    LAC uint16
    CellID uint32

    Serial uint16
}
```

---

# 11. Fecha GPS

Los 6 bytes representan:

```text
year
month
day
hour
minute
second
```

La documentación indica explícitamente:

```text
GPS position packages with date time in 0 time zone
```

Interpretar inicialmente como:

```text
UTC
```

Guardar siempre internamente como `time.Time` UTC.

No aplicar UTC+8.

El UTC+8 pertenecía a la lógica estadística de la plataforma Smake, NO al paquete GPS directo.

---

# 12. Latitud y longitud

Según la documentación:

```text
raw / 30000
```

representa minutos acumulados y debe convertirse correctamente a grados.

Seguir exactamente la fórmula del documento.

Considerar los bits de:

```text
East / West
North / South
```

para determinar signo.

Crear funciones independientes:

```go
func DecodeLatitude(raw uint32, north bool) float64
func DecodeLongitude(raw uint32, east bool) float64
```

Agregar tests usando los ejemplos documentados.

---

# 13. Velocidad

El protocolo entrega:

```text
0..255 km/h
```

No convertir millas.

Guardar:

```text
SpeedKmh
```

---

# 14. Heading y flags GPS

Decodificar los dos bytes de heading/status.

Extraer:

- GPS real-time vs differential;
- GPS positioned;
- East/West;
- North/South;
- heading `0..360`;
- ACC cuando el dispositivo use ese bit.

La documentación advierte que algunos productos no utilizan el bit ACC en este campo.

Por ello:

```go
ACC *bool
```

debe permitir `nil`.

---

# 15. ACK del paquete GPS

IMPORTANTE:

Según la documentación:

```text
0x12 GPS/LBS
```

**NO requiere respuesta del servidor.**

NO enviar ACK para `0x12`.

---

# 16. Recuperación de posiciones históricas

La documentación indica explícitamente que los paquetes `0x12`:

- pueden ser almacenados cuando la señal GSM es anormal;
- son enviados posteriormente;
- conservan su fecha/hora GPS original;
- pueden llegar después de que el vehículo se haya detenido.

Por ello:

NO utilizar:

```go
time.Now()
```

como fecha del posicionamiento.

Mantener dos tiempos separados:

```go
GPSAt
ReceivedAt
```

Ejemplo:

```go
type ReceivedLocation struct {
    Location   LocationPacket
    ReceivedAt time.Time
}
```

Esto será fundamental posteriormente para reconstruir kilometraje después de interrupciones de conectividad.

---

# 17. Heartbeat — protocolo 0x13

Implementar parser para:

```text
Protocol Number: 0x13
```

Extraer al menos:

- terminal information;
- voltage level;
- GSM signal;
- external voltage;
- language;
- product/software key;
- serial.

Crear:

```go
type HeartbeatPacket struct {
    IMEI string

    ACC bool
    GPSLocated bool
    ExternalPower bool

    VoltageLevel uint8
    GSMSignal uint8
    ExternalVoltage uint8

    Serial uint16
}
```

Decodificar correctamente los bits documentados del `Terminal Information`.

---

# 18. ACK Heartbeat

Heartbeat SÍ requiere ACK.

Formato:

```text
78 78
05
13
SERIAL
CRC
0D 0A
```

El serial debe ser exactamente el mismo del heartbeat recibido.

Crear:

```go
func BuildHeartbeatACK(serial uint16) ([]byte, error)
```

La documentación da como ejemplo:

```hex
78 78 05 13 00 01 E9 F1 0D 0A
```

para serial:

```hex
00 01
```

Agregar test exacto.

---

# 19. Alarmas — protocolo 0x16

Implementar parser básico para:

```text
Protocol Number: 0x16
```

Incluye información GPS + LBS + estado + alarma.

Soportar al menos los tipos documentados:

```text
0x00 Normal
0x01 SOS
0x02 Power failure
0x03 Vibration
0x04 Enter geofence
0x05 Exit geofence
0x06 Overspeed
0x09 Displacement
0x0E Low battery
0xFE ACC flameout
0xFF ACC ignition
```

Crear:

```go
type AlarmPacket struct {
    IMEI string

    GPSAt time.Time
    Latitude float64
    Longitude float64
    SpeedKmh uint8

    AlarmType byte

    ACC bool
    GPSLocated bool

    VoltageLevel uint8
    GSMSignal uint8

    Serial uint16
}
```

---

# 20. ACK Alarm

La documentación especifica respuesta del servidor para `0x16`.

Implementarla exactamente según el formato documentado.

Debe utilizar:

- protocol number `0x16`;
- mismo serial;
- CRC recalculado.

Crear:

```go
func BuildAlarmACK(serial uint16) ([]byte, error)
```

Agregar test utilizando los ejemplos del documento.

---

# 21. Protocolos todavía no implementados

El decoder debe reconocer pero NO necesariamente interpretar todavía:

```text
0x15 command response
0x18 LBS multi-base-station
0x1A query address GPS
0x2C LBS + WiFi
0x8D recording
0x90 IMSI
0x94 ICCID
0x80 server command
```

Cuando llegue uno de ellos:

```text
event=unsupported_protocol
protocol=0xXX
raw_hex=...
```

NO cerrar la conexión.

No fallar.

---

# 22. Cabecera 0x79 0x79

Algunos paquetes utilizan:

```hex
79 79
```

y longitud de 2 bytes.

El parser debe diseñarse para que pueda soportarlo posteriormente.

En esta fase:

- detectar `79 79`;
- leer correctamente el length cuando sea posible;
- guardar raw packet;
- registrar protocolo;
- no necesariamente interpretar todos sus contenidos.

No asumir que todos los paquetes usan un byte de longitud.

---

# 23. Logging

Utilizar logging estructurado.

No imprimir únicamente texto libre.

Ejemplo:

```text
level=INFO
event=tracker_login
imei=867111066918647
remote=190.x.x.x:43211
protocol=0x01
serial=143
```

GPS:

```text
event=location
imei=867111066918647
gps_at=2026-08-28T13:45:20Z
received_at=2026-08-28T13:45:22Z
lat=10.500832
lon=-66.841789
speed_kmh=34
satellites=9
```

Siempre registrar temporalmente:

```text
raw_hex
```

durante el PoC.

NO registrar información sensible que no sea necesaria.

---

# 24. Captura RAW

Todo paquete recibido debe poder visualizarse como HEX.

Crear helper:

```go
func Hex(data []byte) string
```

Durante el PoC necesitamos comparar cada mensaje recibido con la documentación.

---

# 25. Manejo de errores

Nunca provocar panic por datos enviados por un tracker.

Errores esperados:

- header inválido;
- longitud inválida;
- CRC inválido;
- frame incompleto;
- protocolo desconocido;
- fecha inválida;
- coordenadas inválidas;
- conexión cerrada;
- timeout;
- serial inválido.

Un paquete inválido NO debe tumbar el servidor.

Una conexión problemática NO debe afectar otras conexiones.

---

# 26. Límites de seguridad

Agregar límites:

```text
max frame size
max buffered bytes per connection
read timeout
write timeout
```

Evitar que una conexión maliciosa consuma memoria indefinidamente.

---

# 27. Tests unitarios

Crear tests para:

### Frame

- frame completo;
- frame dividido en varios reads;
- dos frames en un mismo read;
- header inválido;
- stop bits inválidos;
- packet length inválido.

### CRC

Validar CRC contra los ejemplos documentados.

### Login

Entrada:

```hex
78780d01012345678901234500018cdd0d0a
```

Debe producir IMEI:

```text
123456789012345
```

serial:

```text
1
```

ACK:

```hex
787805010001d9dc0d0a
```

### Heartbeat

Validar ejemplo documentado.

ACK serial 1:

```hex
787805130001e9f10d0a
```

### GPS

Validar:

- timestamp;
- coordenadas;
- velocidad;
- satélites;
- heading;
- flags.

### CRC inválido

El frame debe ser rechazado.

---

# 28. Tests de integración TCP

Crear un test que:

1. levante el servidor en un puerto dinámico;
2. abra conexión TCP;
3. envíe login;
4. reciba ACK;
5. compare byte por byte;
6. envíe heartbeat;
7. reciba ACK;
8. envíe GPS;
9. compruebe que NO se envió ACK;
10. cierre conexión.

---

# 29. Comportamiento esperado de sesión

Flujo esperado:

```text
Tracker conecta
      ↓
0x01 Login
      ↓
Servidor valida
      ↓
ACK 0x01
      ↓
Conexión establecida
      ↓
0x12 GPS
      ↓
parse + log
      ↓
NO ACK
      ↓
0x13 Heartbeat
      ↓
parse
      ↓
ACK 0x13
      ↓
continúa sesión
```

La documentación indica que si el dispositivo no recibe correctamente respuestas de login/heartbeat puede considerar la conexión anormal, desconectarse y reintentar.

Por ello estos ACK son críticos.

---

# 30. README

Documentar:

## Desarrollo

```bash
go test ./...
go run ./cmd/server
```

## Producción inicial

```bash
TALLERP_TELEMETRY_PORT=8899 ./tallerp-telemetry
```

Explicar:

- puerto TCP;
- protocolo;
- logs;
- cómo probar con netcat;
- cómo ejecutar tests.

---

# 31. No persistencia todavía

No crear ninguna base de datos.

Los primeros paquetes reales serán inspeccionados mediante logs.

La persistencia será la siguiente fase una vez comprobemos:

- login;
- ACK;
- heartbeat;
- GPS;
- retransmisión;
- comportamiento real del modelo Smake.

---

# 32. No cálculo de kilometraje todavía

No calcular kilometraje.

Posteriormente Laravel será responsable de:

- almacenar/consumir posiciones;
- calcular kilometraje estimado;
- mantenimiento preventivo.

Go debe concentrarse inicialmente en:

> recibir correctamente la telemetría del dispositivo.

---

# 33. Preparación para comandos futuros

No implementar UI ni comandos todavía.

Sin embargo, la arquitectura debe permitir posteriormente enviar:

```text
0x80 Server Command
```

Los contenidos son strings ASCII compatibles con comandos SMS.

El dispositivo responde mediante:

```text
0x15
```

No acoplar el servidor de manera que esto sea difícil de implementar después.

---

# 34. Calidad

Ejecutar:

```bash
gofmt
go vet ./...
go test ./...
```

Todo debe pasar.

No agregar dependencias externas salvo que exista una razón clara.

Preferir standard library de Go para esta primera fase.

---

# 35. Entregable

Al terminar:

1. mostrar estructura creada;
2. explicar decoder;
3. explicar CRC;
4. explicar ACK login;
5. explicar ACK heartbeat;
6. explicar GPS parser;
7. listar protocolos soportados;
8. listar protocolos pendientes;
9. mostrar resultado de `go test ./...`;
10. proporcionar comando exacto para ejecutar localmente;
11. proporcionar comando para compilar ARM64/Linux:

```bash
GOOS=linux GOARCH=arm64 go build -o tallerp-telemetry ./cmd/server
```

NO desplegar todavía en AWS.

Primero revisaré localmente el código y las pruebas.

---

# Criterios de aceptación de Fase 1

La fase se considera terminada cuando:

- el servidor escucha TCP;
- acepta múltiples conexiones;
- soporta buffering TCP correctamente;
- valida `0x78 0x78`;
- valida longitud;
- valida CRC;
- decodifica login `0x01`;
- obtiene IMEI;
- responde ACK login correcto;
- decodifica GPS `0x12`;
- no responde indebidamente a GPS;
- decodifica heartbeat `0x13`;
- responde ACK heartbeat con mismo serial;
- decodifica alarmas `0x16`;
- responde ACK cuando corresponde;
- conserva `GPSAt` separado de `ReceivedAt`;
- no falla con protocolos desconocidos;
- todos los tests pasan;
- no existe todavía dependencia con Smake API, Laravel ni base de datos.
```
