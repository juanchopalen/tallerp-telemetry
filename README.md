# TallERP Telemetry

Servidor TCP en Go para recibir directamente telemetría de trackers GPS/OBD2
que usan GT06, GT02 y JT808, documentados en `docs/GT06 Protocol 230225.pdf`,
`docs/GT02.pdf` y el manual JT808 V1.0 del fabricante.

Implementa framing TCP y CRC-ITU compartidos para GT06/GT02, y un framing y
despachador propios para JT808 (delimitado por `0x7E`, con byte-stuffing y
checksum XOR — incompatible con el decoder de GT06/GT02). La detección de
protocolo es por conexión y cada familia tiene su handler independiente. Cada
evento válido se guarda primero en un spool SQLite durable y un worker
independiente lo entrega por lotes a la API interna de Laravel. Los ACK del
tracker nunca esperan una respuesta de Laravel.

## Requisitos

- Go 1.27 o posterior.
- Un puerto TCP disponible. El valor predeterminado es `8899`.
- Un directorio escribible para SQLite.

## Desarrollo

Ejecutar todas las pruebas:

```bash
go test ./...
```

Para desarrollo, usar un spool local e iniciar el servidor:

```bash
TALLERP_SPOOL_PATH=./spool.db go run ./cmd/server
```

Elegir otro puerto:

```bash
TALLERP_TELEMETRY_PORT=9000 go run ./cmd/server
```

Los listeners TCP y health se enlazan a `0.0.0.0`. Todas las opciones están en
[`.env.example`](.env.example); el binario lee variables de entorno, no carga
automáticamente archivos `.env`.

## Entrega durable

SQLite usa WAL y conserva eventos pendientes a través de reinicios. Los eventos
entregados se eliminan después de la retención configurada; los pendientes no
se eliminan por antigüedad. El worker envía lotes a
`POST /api/internal/telemetry/events`, acepta IDs nuevos y duplicados como
entregados y reintenta fallos con backoff exponencial y jitter. El contrato que
Laravel debe implementar está en
[`docs/laravel-ingestion-api.md`](docs/laravel-ingestion-api.md).

Endpoints operativos:

- `GET /health`: el proceso está vivo;
- `GET /ready`: el listener TCP está activo y SQLite responde y admite escritura.

La disponibilidad de Laravel no afecta `/ready`.

## Prueba manual

En una terminal, iniciar el servidor. En otra, enviar el login de ejemplo y
mostrar el ACK como hexadecimal:

```bash
printf '78780d01012345678901234500018cdd0d0a' \
  | xxd -r -p \
  | nc 127.0.0.1 8899 \
  | xxd -p
```

La respuesta esperada es:

```text
787805010001d9dc0d0a
```

`0x01`, `0x13` y `0x16` reciben ACK con el mismo serial del tracker. `0x12` no
recibe respuesta, tal como exige el protocolo.

## Logs

Los eventos se escriben como JSON estructurado en stdout. Durante el PoC cada
paquete incluye `raw_hex`. Los eventos de ubicación mantienen separados:

- `gps_at`: hora UTC contenida en el tracker;
- `received_at`: hora UTC de recepción en el servidor.

También se registran IP y puerto remotos, conexión, desconexión, bytes
recibidos, errores de socket, CRC inválido y protocolos no soportados.

## Protocolos

Familias soportadas en el mismo puerto:

- GT06 / N01K;
- GT02 / OBD;
- JT808 / OBD2.

El campo JSON `protocol` conserva el número de mensaje y
`protocol_family` identifica `gt06`, `gt02` o `jt808`. La sesión fija la
familia después del login/registro o de un mensaje exclusivo; no se
redetecta en cada paquete. JT808 se distingue de GT06/GT02 por el primer
byte del stream (`0x7E` vs. `0x78`/`0x79`). La guía GT02 completa está en
[`docs/gt02.md`](docs/gt02.md) y la guía JT808 en
[`docs/jt808.md`](docs/jt808.md).

GT06 interpretado:

- `0x01`: login e IMEI, con ACK;
- `0x12`: GPS/LBS, sin ACK;
- `0x13`: heartbeat, con ACK;
- `0x16`: alarma, con ACK.

GT02 interpretado:

- `0x01`: login con tipo de dispositivo y ACK;
- `0x31`: GPS/LBS UTC, ACC y transmisión suplementaria;
- `0x13`: heartbeat y ACK;
- `0x32`: alarma con ACK `0x26`;
- `0x34`: LBS multibase;
- `0x33`: celdas y WiFi;
- `0x94`: información general, incluido IMEI/IMSI/ICCID;
- `0x21`: respuesta de comando ASCII o UTF-16BE;
- `0x80`: construcción de comandos, sin ejecución automática.

El decoder acepta cabeceras `0x7878` y `0x7979`. Otros mensajes GT06 pendientes se
validan como frames y se registran sin cerrar la conexión:

- `0x15`: respuesta a comando;
- `0x18`: LBS multibase;
- `0x1A`: consulta de dirección GPS;
- `0x2C`: LBS y Wi-Fi;
- `0x80`: comando del servidor;
- `0x8D`: grabación;
- `0x90`: IMSI;
- `0x94`: ICCID e información general.

## Producción con systemd

Compilar el binario local:

```bash
go build -o tallerp-telemetry ./cmd/server
```

Crear el usuario y el directorio durable antes de iniciar el servicio:

```bash
sudo useradd --system --home /var/lib/tallerp-telemetry --shell /usr/sbin/nologin tallerp-telemetry
sudo install -d -o tallerp-telemetry -g tallerp-telemetry -m 0750 /var/lib/tallerp-telemetry
```

El proceso debe ejecutarse como `tallerp-telemetry`. Ese usuario necesita crear
y modificar `spool.db`, `spool.db-wal` y `spool.db-shm`. Cargue las variables de
`.env.example` mediante `EnvironmentFile=` y mantenga el token fuera del
repositorio.

Compilar para Ubuntu ARM64:

```bash
GOOS=linux GOARCH=arm64 go build -o tallerp-telemetry ./cmd/server
```

El proceso responde a `SIGINT` y `SIGTERM`, cierra el listener y termina las
conexiones activas de forma ordenada.
