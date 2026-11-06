# PlayBox VR Control Center

Un solo ejecutable (`playbox.exe`) escrito en Go.

## Aplicación (v0.4)

Doble clic en `playbox.exe` → ventana (WebView2; si falta, se abre en el navegador):
estado de los 10 visores, router e Internet, incidentes y eventos, y botones **Actualizar**,
Configurar IP, Simulación, Descubrimiento F0 y Abrir datos.

Si se abre desde Descargas aparece **Instalar en este PC**: se copia a `%LOCALAPPDATA%\Programs\PlayBox`,
crea accesos en Escritorio y menú Inicio y se registra en «Aplicaciones» de Windows (sin administrador).
Desinstalar desde Configuración → Aplicaciones, o `playbox desinstalar-app`.

El exe se compila como aplicación de ventana (`-H windowsgui`); por línea de órdenes se engancha a la consola.

## v0.1 — Descubrimiento F0 (solo lectura)

Doble clic en `playbox.exe` → menú: reposo, partida de prueba de cada proveedor o cambio de proveedor.
Genera `informes\f0_<fecha>_<fase>\resumen.md` y un `.zip` junto al exe.

Para leer los visores, deja la carpeta `platform-tools` (de Android) junto a `playbox.exe` y conecta el visor por USB.

Línea de órdenes:

```
playbox descubrir --fase partida-vex --ping-n 60
playbox descubrir --sin-adb --sin-ping
playbox version
```

No modifica nada: no instala, no borra, no reinicia y no cambia ajustes del PC, visores ni router.

## Actualización automática (v0.2)

Al arrancar, playbox lee `https://raw.githubusercontent.com/marcvilap/playbox-control-center/actualizaciones/version.json`. Si hay versión nueva, descarga el exe,
comprueba su SHA-256 y la firma ed25519 (clave pública compilada en el programa), se sustituye y se relanza.
Sin conexión o con firma no válida, sigue con la versión instalada. `playbox actualizar` fuerza la comprobación.

### Publicar una versión nueva

1. Sube `Version` en `main.go` y compila el exe.
2. `go run ./herramientas/publicar -clave clave-firma-playbox.txt -exe dist/playbox.exe -version X.Y.Z -url https://raw.githubusercontent.com/marcvilap/playbox-control-center/actualizaciones/playbox-X.Y.Z.exe -notas "…"`
3. Publica `playbox-X.Y.Z.exe` y `version.json` en la rama `actualizaciones` (solo guarda la última versión).

La clave privada no se sube nunca al servidor ni al repositorio.

## Vigilancia continua (v0.3)

- `playbox simular` — 10 visores virtuales con un guion de fallos (PICO-03 cae, PICO-07 va lento, Internet cae). No toca nada real.
- `playbox configurar` — asigna la IP de cada PICO; si hay un visor por USB, propone su IP.
- `playbox vigilar` — vigila en una ventana (Ctrl+C para salir).
- `playbox servicio instalar` — servicio Windows «PlayBox VR — Vigilancia», arranque automático, prioridad baja, se reinicia solo si falla. Se actualiza solo cuando el centro lleva 10 min sin visores activos.
- `playbox estado` — muestra el estado guardado por el servicio.

Datos en `datos\`: `estado.json`, `eventos\eventos-AAAA-MM-DD.jsonl` (caja negra, 30 días) e `incidentes\*.json`
(expediente con 5 min antes y 2 después, estado, eventos, muestras e hipótesis con nivel de confianza).

Estados: ONLINE (responde a ping), OFFLINE (3 fallos seguidos), SIN CONFIRMACIÓN (aún sin datos), SIN CONFIGURAR (sin IP).
ONLINE no confirma app abierta ni unión a partida. Un visor que cae mientras otros siguen activos abre incidente;
si se apagan todos, se registra como probable cierre.

## Compilar

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/playbox.exe .
go test ./...
```

## Estructura

- `main.go` — subcomandos; `app.go` + `ui/index.html` — ventana y API local; `instalar_app_windows.go` — instalador; `vigilar.go`, `configurar.go`, `servicio_windows.go`, `actualizacion.go`
- `internal/ejec` — ejecución de comandos con límite de tiempo
- `internal/descubrir` — F0: Windows, proveedores, visores (ADB), red, informe
- `internal/monitor` — vigilancia, caja negra, incidentes, eventos de Windows
- `internal/sonda` — ping ICMP nativo (IcmpSendEcho) y sonda simulada
- `internal/actualizar` — autoactualización firmada

## Próximas versiones

- v0.5 alertas por Telegram
