# TallERP Telemetry

Servidor TCP en Go para recibir directamente telemetría de trackers GPS que
usan la variante GT06 documentada en `docs/GT06 Protocol 230225.pdf`.

La primera fase implementa framing TCP, CRC-ITU, login (`0x01`), posiciones
GPS/LBS (`0x12`), heartbeat (`0x13`) y alarmas (`0x16`). No usa base de datos,
Laravel, Smake API ni una API HTTP.

## Requisitos

- Go 1.27 o posterior.
- Un puerto TCP disponible. El valor predeterminado es `8899`.

## Desarrollo

Ejecutar todas las pruebas:

```bash
go test ./...
```

Iniciar el servidor en el puerto predeterminado:

```bash
go run ./cmd/server
```

Elegir otro puerto:

```bash
TALLERP_TELEMETRY_PORT=9000 go run ./cmd/server
```

El listener se enlaza a `0.0.0.0:<puerto>`. La variable debe contener un entero
entre `1` y `65535`.

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

Interpretados en esta fase:

- `0x01`: login e IMEI, con ACK;
- `0x12`: GPS/LBS, sin ACK;
- `0x13`: heartbeat, con ACK;
- `0x16`: alarma, con ACK.

El decoder acepta cabeceras `0x7878` y `0x7979`. Los protocolos pendientes se
validan como frames y se registran sin cerrar la conexión:

- `0x15`: respuesta a comando;
- `0x18`: LBS multibase;
- `0x1A`: consulta de dirección GPS;
- `0x2C`: LBS y Wi-Fi;
- `0x80`: comando del servidor;
- `0x8D`: grabación;
- `0x90`: IMSI;
- `0x94`: ICCID e información general.

## Producción inicial

Compilar el binario local:

```bash
go build -o tallerp-telemetry ./cmd/server
```

Ejecutarlo:

```bash
TALLERP_TELEMETRY_PORT=8899 ./tallerp-telemetry
```

Compilar para Ubuntu ARM64:

```bash
GOOS=linux GOARCH=arm64 go build -o tallerp-telemetry ./cmd/server
```

El proceso responde a `SIGINT` y `SIGTERM`, cierra el listener y termina las
conexiones activas de forma ordenada.

