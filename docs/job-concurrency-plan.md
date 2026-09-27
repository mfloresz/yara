# Plan de concurrencia de jobs por proveedor y origen de descarga

**Estado:** implementado (rama `feat/job-concurrency`). Claves globales por host y por proveedor, un job activo por novela (rechazo 409 en admisión + clave exclusiva en el despachador), cupos globales por clase (IA/web).

## Objetivo y alcance

Permitir que jobs de novelas distintas avancen en paralelo cuando no compitan por el mismo proveedor de IA ni por el mismo sitio de descarga, sin permitir escrituras simultáneas sobre una novela. Conservar la semántica de jobs `pending`/`running`/`done`/`failed`/`cancelled`, las respuestas de `/api/v1/*` y la recuperación tras reiniciar el servidor.

Incluye `translate`, `refine`, `generate-glossary`, `download` (importación, actualización y redescarga) y `check` (consulta del sitio). No cambia la concurrencia de capítulos dentro de un job ni promete limitar peticiones HTTP síncronas de previews/importación: estas no pasan por las colas de jobs.

## Estado actual y riesgos

- `internal/api/runtime_worker.go` tiene dos colas de 128 IDs, con **un consumidor por cola**. `download` y `check` van a la cola web; el resto, a la de IA. `queuedJobs` deduplica por ID, no por recurso. `StopJobWorker` drena las colas y espera a ambos consumidores.
- `internal/store/store_jobs.go` persiste los jobs y recupera hasta 500 `pending`/`running` al iniciar. La clasificación después de un reinicio debe repetir la misma resolución de recursos; no debe depender de un mapa de locks previo.
- `job.Provider` puede estar vacío. `internal/api/runtime_config.go` resuelve el proveedor de contenido a partir del usuario, la novela y el override del job; el proveedor de títulos puede ser distinto. `generate-glossary` tiene su propia resolución en `internal/api/runtime_glossary.go`. La configuración puede cambiar mientras un job espera.
- `internal/api/router_import.go` guarda en `OptionsJSON` la URL de la novela y, para `download`, las URLs de los capítulos. Hay redirecciones/espejos posibles: la URL configurada no siempre identifica el destino HTTP final.
- `internal/api/router_jobs.go` y los flujos batch pueden crear jobs que afecten a los mismos capítulos. Dos actualizaciones de la misma novela pueden planificar capítulos duplicados; reasignar un `chapter_order` ocupado en `processDownloadJob` evita una colisión del índice, **no** evita duplicados de contenido. La redescarga sí comprueba jobs activos de esa novela, pero solo en su propio flujo.
- El progreso y las estadísticas se escriben durante/después de la ejecución. La cancelación no es una barrera contra escrituras de peticiones ya iniciadas. Habilitar más jobs simultáneos sin proteger esas transiciones aumenta las carreras.

## Política propuesta de recursos

| Recurso | Clave propuesta | Exclusión |
| --- | --- | --- |
| IA | ID de cada proveedor lógico efectivo usado por el job, incluido el de títulos cuando corresponda | Un job activo por proveedor, global entre usuarios; modelos diferentes del mismo proveedor esperan. `generate-glossary` reserva el proveedor que realmente usa. |
| Sitio | Origen normalizado (`scheme://host[:port]`) de `options.url` y de las URLs de capítulos conocidas | Un job web activo por origen; un job que abarque varios orígenes reserva todos. |
| Escrituras sobre novela | ID de novela | Un job activo por novela, aunque use otro proveedor/origen; incluye `check` y glosario porque también modifican estado de novela o caché. |
| Capacidad global | Cupos acotados e independientes para IA y web | Evitan goroutines/solicitudes ilimitadas incluso con muchas claves distintas. Los valores iniciales deben fijarse y medirse antes del despliegue. |

Todas las claves necesarias se reservan **atómicamente como conjunto** antes de lanzar el job y se liberan al terminar por cualquier camino. Si una clave está ocupada, el job sigue `pending`; no se toma un lock mientras espera ni se bloquea el despachador. Se puede escoger otro job elegible posterior sin perder el orden FIFO **entre jobs que comparten una clave**, evitando bloqueo de toda la cola por el primer job ocupado.

Esta política es deliberadamente conservadora para novelas que tienen trabajos distintos. La exclusión por origen se refiere a **jobs**, no garantiza un límite global de peticiones: previews, primer capítulo de importación y el browser-worker tienen vías propias; las redirecciones pueden alcanzar un origen no conocido al planificar. Si el requisito es un límite estricto por sitio, habría que coordinar cada petición en el cliente HTTP/proxy además del scheduler.

## Implementación por etapas

### 1. Definir identidad y clasificación del job

- En `internal/api/runtime_worker.go`, clasificar explícitamente `translate`/`refine`/`generate-glossary` como IA y `download`/`check` como web; no dejar que una operación desconocida caiga implícitamente en traducción.
- Para IA, resolver configuración y proveedor(es) **una sola vez para esa ejecución** y pasarla al procesador. No reservar según `job.Provider` para luego usar otra configuración dentro de `processJob`. Si la configuración cambia mientras espera, resolverla de nuevo antes de reservar; los cambios posteriores afectan al siguiente job, no al ya iniciado. Reservar conservadoramente el proveedor de títulos aunque su creación acabe recurriendo al de contenido.
- Para web, leer la instantánea en `OptionsJSON` y obtener los orígenes mediante `net/url`: aceptar solo URL HTTP(S) con host, normalizar esquema/host y puerto por defecto; ignorar ruta, query y fragmento. Si la instantánea es inválida, fallar el job con un error visible, no ejecutarlo sin clave. No registrar credenciales ni claves de API.
- No añadir campos de colección ni migración: las claves se derivan del job y de la configuración al ejecutarlo. Documentar que un job recuperado tras reinicio puede usar la configuración de proveedor vigente, igual que hoy.

**Verificación:** tests de clasificación para override vacío, proveedor de novela, títulos con otro proveedor, glosario, `check`, varias URLs de capítulos, puertos equivalentes y URL inválida.

### 2. Sustituir el consumidor único por despacho acotado

- Mantener un punto único de admisión por ID (`enqueueJob`) y el comportamiento de cola saturada. Usar un despachador por tipo que gestione pendientes, claves ocupadas y cupos; no crear un worker fijo por proveedor/host ni una goroutine por job pendiente.
- Seleccionar un job elegible, reservar **todas** sus claves bajo el mismo estado sincronizado, arrancar la ejecución y liberar en `defer`. No retener el mutex del planificador durante acceso a PocketBase, peticiones de red ni esperas.
- Mantener la deduplicación de IDs durante espera **y ejecución**, comprobar el estado persistido antes de arrancar, y despertar el despachador cuando termina/cancela un job. Los `pending` que esperan por una clave no deben recibir un 503; el 503 permanece para la cola realmente llena.
- Adaptar `StopJobWorker`: dejar de admitir nuevos jobs, despertar/drenar pendientes según el comportamiento vigente, esperar todos los jobs en vuelo y solo entonces cerrar el store. Revisar en este paso el límite de 500 jobs recuperados para no dejar pendientes persistidos sin volver a encolar.

**Verificación:** barreras/canales en tests (no depender de tiempos): mismo proveedor u origen nunca se solapan; proveedores/orígenes distintos sí; un job bloqueado no impide despachar otro independiente; cupos máximos, cola llena, cancelación en espera y apagado/reinicio.

### 3. Proteger estado compartido por novela

- Coordinar **todas** las rutas que crean trabajos de la misma novela (`router_jobs.go`, `router_import.go` y `router_glossary.go`) con la exclusión existente `lockNovel` cuando comprueben conflictos y creen/encolen; incluir variantes batch. Mantener la respuesta 409 de redescarga si hay un trabajo incompatible activo. Decidir expresamente si dos `update` simultáneos se rechazan o se fusionan; la opción mínima es rechazarlos.
- Serializar la ejecución por novela, además de los recursos IA/web. Para descargas planificadas antes de que cambie la biblioteca, contrastar capítulos existentes antes de guardar: ante una identidad ambigua, fallar o saltar con feedback explícito; **no** convertir una descarga repetida en otro capítulo por el mero hecho de desplazar su orden. Valorar persistir URL de origen por capítulo solo si resulta imprescindible para deduplicación fiable; eso requeriría diseño de esquema/migración separado.
- Revisar `ReconcileProcessingChaptersForJob`, la persistencia de progreso en `runtime_translate.go` y el recálculo de estadísticas para que cancelar o terminar un job no sobrescriba estado perteneciente a otro. Comprobar el contexto antes de guardar resultados obtenidos tras una petición larga; la cancelación de fetches del browser-worker puede continuar siendo tardía y debe señalarse, no asumirse inmediata.

**Verificación:** dos updates de la misma novela, batch-update frente a update, redescarga frente a traducción, cancelación con trabajo en vuelo, capítulos sin duplicar y estadísticas finales correctas.

### 4. Observabilidad y validación final

- Registrar con `slog` el ID de job, tipo, tiempo en espera, inicio, fin y motivo de bloqueo/liberación, sin secretos ni URL con query sensible. `GET /api/v1/jobs/active` debe seguir reflejando jobs `pending` y `running`; no añadir otro estado público sin actualizar cliente, API y tests.
- Ejecutar primero los tests de planificador y flujo de jobs; después `go test -short ./internal/api/... ./internal/store/... ./internal/noveldownloader/...` y, cuando el entorno lo permita, los tests de concurrencia con `-race`. Verificar el ciclo de apagado/reinicio y la compilación con `go test -short ./...`.

## Criterios de aceptación

1. Dos jobs elegibles de novelas distintas y claves IA/web distintas pueden estar `running` a la vez; con una clave compartida, como máximo uno lo está.
2. Nunca hay dos ejecutores de jobs para la misma novela, ni se crean capítulos duplicados al repetir una actualización mientras hay trabajo activo o una planificación obsoleta.
3. Cancelar un job en espera libera su plaza sin ejecutarlo; cancelar uno activo termina sin resucitarlo a `done`. La capacidad total de ejecución permanece acotada.
4. Un job bloqueado no detiene a otros independientes; saturación, retry, parada y recuperación conservan los contratos existentes de estado y respuesta HTTP.
5. Los tests automatizados reproducen exclusión, paralelismo real y los casos de fallo, sin depender de `time.Sleep` para demostrar simultaneidad.

## Decisiones pendientes antes de escribir código

1. **«URL distinta»:** ¿URL completa por novela o **origen del sitio**? Se propone origen para evitar peticiones paralelas al mismo sitio aun cuando sean novelas/rutas diferentes. Espejos y redirecciones no quedan cubiertos de forma absoluta por una clave calculada al arrancar el job.
- Respuesta: Si, por url host origen, por ejemplo novelfire.net. Este filtro es global, es decir, no importa si son diferentes usuarios o novela, solo se permite un job por host.
2. **«Proveedor distinto»:** ¿ID lógico global (propuesta conservadora), `usuario + proveedor` o credencial/cuenta efectiva? Claves compartidas entre usuarios hacen inseguro suponer que usuarios diferentes tienen capacidad independiente; la opción más precisa implica identificar la credencial sin exponer el secreto.
- Respuesta: Lo mismo que el anterior, es global, ya que las restricciones de api son por proveedor.
3. **Capacidad máxima:** determinar cupos simultáneos de IA y web según recursos de SQLite, red, proveedor y browser-worker. Los límites `1..10` configurados hoy para capítulos son **por job** y no sustituyen estos cupos globales.
- Respuesta: Analizar cual es la capacidad máxima o si se requiere un engine que encole las escrituras, asi aunque haya muchas tareas, el engine se encarge de pasarle secuencialmente o paralelamente pero en menor numero, las escrituras.
4. **Jobs de una misma novela:** la propuesta los serializa todos, aunque usen recursos externos distintos. Si se exige paralelismo entre capítulos disjuntos de una misma novela, habría que diseñar propiedad de capítulo, escrituras condicionadas y reconciliación por job antes de habilitarlo.
- Respuesta: Por novela, solo puede haber un job, es decir, si la novela ya está en proceso, no se puede iniciar otro job para ella, ya sea traducción o descarga.
