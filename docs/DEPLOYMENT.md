# Despliegue de tallerp-telemetry

Esta guía describe el despliegue manual y reversible de una nueva versión del
servicio de telemetría. El binario se compila y valida en la máquina local. En
el servidor solamente se carga el artefacto resultante; no se clona el
repositorio ni se compila código.

## Convenciones

Los ejemplos asumen:

- servidor Ubuntu ARM64;
- servicio systemd `tallerp-telemetry`;
- ejecutable público `/usr/local/bin/tallerp-telemetry`;
- releases en `/opt/tallerp-telemetry/releases`;
- spool SQLite `/var/lib/tallerp-telemetry/spool.db`;
- listener TCP en el puerto `8899`;
- health HTTP en el puerto `8080`.

Cambie los valores si la unidad systemd o su `EnvironmentFile` usan otros
puertos o rutas.

## 1. Verificaciones previas

Antes de desplegar:

1. Confirme que cualquier cambio requerido por el contrato de la API de
   TallERP ya sea compatible con el binario nuevo.
2. Confirme que no haya una incidencia activa en la recepción o entrega de
   telemetría.
3. Registre el release actualmente activo:

```bash
sudo systemctl status tallerp-telemetry --no-pager
sudo systemctl show tallerp-telemetry -p ExecStart -p EnvironmentFiles
readlink -f /usr/local/bin/tallerp-telemetry
```

No es necesario detener el servicio para cargar el nuevo binario.

## 2. Compilar y validar localmente

Desde la raíz del repositorio, ejecute las validaciones:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Defina la versión y compile para Linux ARM64. Use una versión única por
despliegue; se recomienda la fecha ISO del release:

```bash
VERSION=2026-09-15
ARTIFACT="tallerp-telemetry.${VERSION}"

GOOS=linux GOARCH=arm64 go build -o "$ARTIFACT" ./cmd/server
```

Compruebe el artefacto y genere su checksum:

```bash
file "$ARTIFACT"
shasum -a 256 "$ARTIFACT"
```

`file` debe mostrar un ejecutable ELF de 64 bits para ARM aarch64. Conserve el
SHA-256 para compararlo después de la transferencia.

## 3. Cargar únicamente el binario

Sustituya `usuario@servidor` por el destino real:

```bash
scp "$ARTIFACT" usuario@servidor:/tmp/"$ARTIFACT"
```

No cargue archivos `.env`, tokens, el spool ni el repositorio.

## 4. Validar el artefacto en el servidor

Conéctese por SSH y defina los nombres correspondientes al nuevo release:

```bash
VERSION=2026-09-15
RELEASE=20260915
SOURCE="/tmp/tallerp-telemetry.${VERSION}"
RELEASE_DIR="/opt/tallerp-telemetry/releases/${RELEASE}"
NEW_BINARY="${RELEASE_DIR}/tallerp-telemetry"
```

Compruebe que el archivo existe, su arquitectura y checksum:

```bash
ls -lh "$SOURCE"
file "$SOURCE"
sha256sum "$SOURCE"
```

El SHA-256 debe coincidir con el calculado en la máquina local. No continúe si
la arquitectura o el checksum son incorrectos.

## 5. Instalar el release sin sobrescribir versiones anteriores

Capture y muestre el destino activo; será el punto de rollback:

```bash
OLD_TARGET="$(readlink -f /usr/local/bin/tallerp-telemetry)"
echo "Release anterior: $OLD_TARGET"
test -x "$OLD_TARGET"
printf '%s\n' "$OLD_TARGET" > /tmp/tallerp-telemetry.previous-target
```

Instale el binario nuevo con propietario y permisos explícitos:

```bash
sudo mkdir -p "$RELEASE_DIR"
sudo install -o root -g root -m 0755 "$SOURCE" "$NEW_BINARY"

sha256sum "$SOURCE" "$NEW_BINARY"
```

Los dos hashes deben ser idénticos.

## 6. Activar y reiniciar

Cree un enlace temporal y muévalo sobre el enlace público. El `mv` mantiene el
cambio de release atómico:

```bash
sudo ln -sfn "$NEW_BINARY" /usr/local/bin/tallerp-telemetry.next
sudo mv -Tf /usr/local/bin/tallerp-telemetry.next /usr/local/bin/tallerp-telemetry

readlink -f /usr/local/bin/tallerp-telemetry
```

Reinicie y compruebe el servicio:

```bash
DEPLOY_STARTED="$(date --iso-8601=seconds)"

sudo systemctl restart tallerp-telemetry
sudo systemctl status tallerp-telemetry --no-pager
```

No ejecute `systemctl daemon-reload` cuando solamente cambie el binario. Es
necesario únicamente si se modificó la unidad systemd o un archivo `drop-in`.

## 7. Verificación técnica

### Proceso y ejecutable

```bash
systemctl is-active tallerp-telemetry

PID="$(systemctl show tallerp-telemetry -p MainPID --value)"
echo "PID=$PID"
sudo readlink -f "/proc/$PID/exe"
```

El servicio debe estar `active` y `/proc/$PID/exe` debe apuntar al release
nuevo.

### Health, readiness y puertos

```bash
curl --fail --show-error http://127.0.0.1:8080/health
curl --fail --show-error http://127.0.0.1:8080/ready
sudo ss -lntp | grep tallerp-telemetry
```

Las respuestas esperadas son:

```json
{"status":"ok"}
{"status":"ready"}
```

`/ready` comprueba tanto el listener TCP como la escritura en SQLite. La
disponibilidad de la API de TallERP se valida por separado mediante la entrega
de eventos.

### Errores desde el reinicio

```bash
sudo journalctl \
  -u tallerp-telemetry \
  --since "$DEPLOY_STARTED" \
  --no-pager
```

Filtre únicamente niveles y condiciones realmente graves:

```bash
sudo journalctl \
  -u tallerp-telemetry \
  --since "$DEPLOY_STARTED" \
  --no-pager |
grep -Ei '"level":"ERROR"|panic|fatal|CRITICAL'
```

No busque solamente la palabra `failure`: las métricas incluyen campos como
`delivery_failure` aun cuando su valor sea cero.

## 8. Prueba funcional con trackers reales

Observe login, heartbeat, ubicación y entrega:

```bash
sudo journalctl -u tallerp-telemetry -f -o cat |
grep --line-buffered -E \
'"event":"(login|location|heartbeat|delivery_success)"|"level":"ERROR"|CRITICAL'
```

Reinicie un tracker de prueba o solicite una ubicación inmediata mediante el
procedimiento admitido por ese dispositivo. No envíe frames con IMEI ficticios
en producción: los eventos de login se persisten y podrían quedar rechazados
en el spool.

Para cada familia habilitada que esté disponible, confirme:

- `protocol_family` correcto (`gt06`, `gt02` o `jt808`);
- IMEI o identificador esperado;
- al menos un login o heartbeat posterior al ACK;
- una ubicación con fecha y coordenadas razonables;
- un evento `delivery_success` con `delivered` mayor que cero;
- ausencia de nuevos `pending_retry`, CRC inválidos o fallos de detección.

Un contador en cero para una familia solo significa que todavía no se ha
conectado un equipo de esa familia; no demuestra un fallo del parser.

## 9. Comprobar el spool sin instalar sqlite3

La imagen del servidor puede no incluir el cliente `sqlite3`. Python permite
consultar el spool en modo de solo lectura:

```bash
sudo python3 - <<'PY'
import sqlite3

db = sqlite3.connect(
    "file:/var/lib/tallerp-telemetry/spool.db?mode=ro",
    uri=True,
)

for row in db.execute("""
    SELECT status, COUNT(*), MIN(attempts), MAX(attempts)
    FROM telemetry_spool
    GROUP BY status
    ORDER BY status
"""):
    print(row)
PY
```

No exija que el total pendiente sea cero si ya existían eventos rechazados
antes del despliegue. Compare con la línea base y compruebe que los eventos
nuevos sean aceptados y que el pendiente no crezca continuamente.

## 10. Criterios de aceptación

El despliegue se considera correcto cuando:

- el proceso activo ejecuta el release nuevo;
- `/health` y `/ready` responden correctamente;
- los puertos configurados están escuchando;
- no hay errores graves desde el reinicio;
- al menos un tracker real vuelve a conectarse y transmite;
- TallERP acepta eventos generados después del despliegue;
- el spool no acumula nuevos rechazos;
- cada familia de protocolo crítica fue probada con hardware real o quedó
  explícitamente marcada como pendiente.

## 11. Rollback

Si falla cualquiera de las verificaciones, restaure el enlace al valor mostrado
en `OLD_TARGET` y reinicie:

```bash
test -n "$OLD_TARGET"
test -x "$OLD_TARGET"

sudo ln -sfn "$OLD_TARGET" /usr/local/bin/tallerp-telemetry.next
sudo mv -Tf /usr/local/bin/tallerp-telemetry.next /usr/local/bin/tallerp-telemetry
sudo systemctl restart tallerp-telemetry

sudo systemctl status tallerp-telemetry --no-pager
sudo readlink -f "/proc/$(systemctl show tallerp-telemetry -p MainPID --value)/exe"
curl --fail --show-error http://127.0.0.1:8080/ready
```

Si se perdió la variable de shell, recupérela del archivo creado antes de la
activación y vuelva a validarla:

```bash
OLD_TARGET="$(cat /tmp/tallerp-telemetry.previous-target)"
echo "Rollback hacia: $OLD_TARGET"
test -x "$OLD_TARGET"
```

No elimine el release nuevo, el artefacto en `/tmp` ni el release anterior
hasta terminar el diagnóstico.

## 12. Cierre del despliegue

Registre en la bitácora operativa:

- versión y fecha;
- SHA-256 desplegado;
- release anterior;
- hora del reinicio;
- resultado de `/health` y `/ready`;
- protocolos y trackers probados;
- contadores de entrega y spool;
- incidencias, rollback o validaciones pendientes de hardware.

Después de un periodo de observación satisfactorio se puede retirar el archivo
temporal de `/tmp`. Conserve al menos el release anterior para rollback.
