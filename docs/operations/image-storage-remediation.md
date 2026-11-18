# Remediación offline de imágenes duplicadas de novelas

Esta operación es **manual, local y con el servicio detenido**. No ejecutar sobre
una instancia activa ni sobre datos remotos/S3. La herramienta usa solamente Python
3 y su biblioteca estándar; no instala programas, no arranca Yara y no migra SQLite.
Los ejemplos son plantillas para una futura ventana de mantenimiento: reemplazar
las rutas antes de ejecutarlos. No se han aplicado a datos reales como parte del cambio.

## Qué cambia y qué conserva

PocketBase guarda cada imagen en
`data/storage/<collectionID>/<recordID>/<filename>`. Varios registros de capítulos
pueden contener los mismos bytes. `tools/deduplicate-novel-images.py` localiza el ID
de `novel_images` mediante `_collections`, lee `id`, `novel` y `file` de esa colección
y sustituye duplicados por **hardlinks locales**, exclusivamente dentro de la misma
novela y del mismo dispositivo de archivos.

- Conserva todas las filas de la base, los nombres, las rutas y las URLs existentes.
- Usa tamaño y SHA-256 como filtro y compara los bytes antes de enlazar. Una colisión
  de hash no basta para deduplicar.
- Cada ruta sigue existiendo. Borrar un registro/capítulo mediante PocketBase elimina
  su enlace, no los enlaces de otros capítulos; el contenido desaparece al eliminar
  el último enlace. No se necesita cambiar el cascade-delete de la base.
- No enlaza novelas diferentes, ni archivos distintos, ausentes, symlinks, directorios
  o archivos especiales. Tampoco sigue symlinks en los directorios de las rutas.
- Exige iguales permisos, propietario, grupo y atributos extendidos (incluidos ACL
  si están expuestos como xattrs). Las diferencias en estos metadatos se notifican
  y se omiten: **no los normaliza** para conseguir más deduplicación.
- Permite `mtime` distintos, algo habitual en copias importadas en momentos diferentes.
  **Sólo al aplicar**, cada duplicado enlazado adopta el `mtime` del archivo canónico
  (el primer registro compatible por novela e ID de registro). No modifica el tiempo
  del canónico ni el respaldo independiente: éste conserva los tiempos anteriores si
  se creó con `copy2`, como en el ejemplo, y permite recuperarlos mediante rollback.
  El dry-run no iguala fechas. `ctime`, número de enlaces y tiempos de directorio
  cambian al enlazar; las lecturas
  también pueden actualizar `atime`, según el sistema de archivos.
- Cada reemplazo usa `os.link` a un nombre temporal y `os.replace` atómico en el mismo
  directorio. Un fallo puede dejar un subconjunto ya deduplicado, nunca una transacción
  global; se informa como resultado parcial. Una interrupción abrupta puede dejar un
  `.dedup-*`; revisar esos archivos offline antes de retirarlos, sin borrar originales.
- Las imágenes enlazadas deben tratarse como **inmutables**: escribir sobre un inode
  existente modifica todas sus rutas. Borrar una ruta o reemplazarla atómicamente
  no cambia las demás. No usar editores ni procesos que sobrescriban imágenes in situ.

La herramienta no puede probar que todos los procesos externos estén detenidos:
`--offline` es una declaración explícita del operador, no un detector de procesos
ni una protección frente a otro escritor. No ejecutar varias copias a la vez.

## Prevención en nuevas importaciones y copias

El servidor limita cada imagen inline a **20 MiB** y el total de escrituras de
imágenes de cada importación EPUB/ZIP o copia de novela a **64 MiB**. Cuenta **cada
referencia de capítulo**, incluso si comparte los mismos bytes con otra. La
comprobación sucede antes de crear la novela o subir archivos; si no cabe, se
rechaza la operación con un error de límite de almacenamiento, sin dejar una
novela parcial por ese rechazo. Al copiar datos antiguos, el presupuesto se
comprueba leyendo en streaming y antes de crear el clon.

Este límite es conservador también en disco local: una ilustración de 1 MiB
referenciada por 65 capítulos supera el presupuesto, aunque pudiera terminar
compartiendo inode. Evita la amplificación ilimitada tanto en S3 como en sistemas
sin hardlinks. El límite se refiere a imágenes inline por operación, no al tamaño
total de la biblioteca, las portadas o el archivo EPUB/ZIP original.

En almacenamiento local compatible, las imágenes idénticas importadas comparten
inodes dentro de cada novela. Si S3 está activo o los hardlinks no están disponibles,
se conservan copias independientes bajo el mismo presupuesto de 64 MiB; un fallo
local de enlace se registra como advertencia y no elimina imágenes. No se desactiva
S3. Esta herramienta de remediación sólo recupera espacio local, no objetos S3.

La exportación comparte en memoria un único blob por contenido SHA-256 idéntico,
también con copias antiguas o almacenamiento remoto; conserva los metadatos y
referencias de cada capítulo. No deduplica las entradas de imagen del EPUB final
ni impone aquí un límite al total de imágenes únicas de una novela ya existente.

## 1. Preparar y detener todos los escritores

1. Identificar el directorio de datos real de la instancia (`--data-dir`) y su cuenta
   de servicio. No asumir que es el `data/` del checkout ni el situado junto al binario.
2. Reservar una ventana de mantenimiento. Detener Yara mediante el supervisor usado
   por esa instalación (servicio, contenedor o proceso), y detener también workers,
   importadores, cron y sincronizadores que escriban en ese directorio. Desactivar el
   reinicio automático mientras dura la ventana.
3. Verificar que los procesos se han detenido, que ya no hay listener de esa instancia
   y que no queda ningún proceso con archivos abiertos en los datos. Usar las
   herramientas del sistema disponibles; un endpoint que no responde por sí solo
   **no prueba** ausencia de escritores. No cambiar un servicio ajeno por compartir puerto.
4. Mantener los escritores detenidos durante checkpoint, respaldo, auditoría,
   aplicación y verificación offline.

La herramienta abre `data.db` como `mode=ro&immutable=1`, sin crear locks, SHM ni
journal. Para no ignorar transacciones pendientes, **rechaza un `data.db-wal` o
`data.db-journal` no vacío incluso en dry-run**. No borrar esos archivos a mano.
Si quedan después de detener la instancia, recuperar/checkpoint SQLite offline
antes del respaldo, utilizando una conexión SQLite normal. Por ejemplo, sólo
cuando se haya verificado que no hay escritores:

```bash
python3 - /ruta/instancia/data <<'PY'
from contextlib import closing
from pathlib import Path
import sqlite3
import sys
path = Path(sys.argv[1]).absolute() / 'data.db'
with closing(sqlite3.connect(path.as_uri() + '?mode=rw', uri=True)) as db:
    checkpoint = db.execute('PRAGMA wal_checkpoint(TRUNCATE)').fetchone()
    if checkpoint[0] != 0:
        raise SystemExit('Checkpoint ocupado: investigar, no continuar')
    result = db.execute('PRAGMA integrity_check').fetchall()
    if result != [('ok',)]:
        raise SystemExit('SQLite no supera integrity_check: investigar, no continuar')
print('Checkpoint e integridad SQLite: correctos')
PY
```

Este paso de preparación **sí puede escribir en SQLite**; no forma parte del dry-run
de la herramienta. Si no finaliza correctamente, no continuar ni eliminar journals.

## 2. Hacer y validar un respaldo completo independiente

El respaldo obligatorio debe ser una **copia existente del directorio de datos
completo**, con todos los SQLite, almacenamiento, configuración, directorios vacíos
y claves locales (`app.key`, si existe). No basta copiar imágenes, exportar sólo la
base ni usar una API de backup mientras el servicio funciona. Guardarlo fuera de
`data`, sin que ninguna de las dos raíces contenga a la otra.

Ejemplo de copia offline con Python estándar (el destino debe no existir):

```bash
python3 - /ruta/instancia/data /ruta/respaldos/yara-antes-dedup/data <<'PY'
from pathlib import Path
import shutil
import sys
source, backup = [Path(p).absolute() for p in sys.argv[1:]]
if source == backup or source in backup.parents or backup in source.parents:
    raise SystemExit('El respaldo debe estar fuera de data y no contenerlo')
shutil.copytree(source, backup, copy_function=shutil.copy2, symlinks=True)
print('Copia offline completa terminada; conservar los permisos de acceso privados')
PY
```

No usar `cp -al`, `os.link`, `rsync --link-dest` ni otro respaldo con hardlinks que
compartan inodes con los originales. Una copia CoW/reflink independiente es válida
si tiene inodes distintos. La herramienta compara inventario completo, tamaños,
SHA-256 y bytes de **cada archivo** y rechaza cualquier inode de respaldo compartido
con cualquier original, incluso bajo otro nombre. Rechaza symlinks/archivos especiales
en cualquiera de los árboles. Esta validación puede tardar y leer todo el respaldo.
No guardar informes o logs nuevos dentro de `data` después de copiarlo.

Si se usa `APP_ENCRYPTION_KEY` externo en lugar de `app.key`, conservar también esa
clave mediante el mecanismo seguro propio de la instalación y comprobar que podrá
restaurarse. Lo mismo aplica a secretos/configuración externos necesarios para
arrancar la instancia. **No pegarlos en terminales compartidos, informes ni logs.**
La herramienta sólo puede validar los archivos dentro de `data`, no esos secretos
externos. Restringir el acceso al respaldo: contiene credenciales y material privado.

## 3. Auditar sin aplicar cambios

Desde la raíz del checkout, usando el mismo usuario que posee los archivos:

```bash
python3 tools/deduplicate-novel-images.py \
  --data-dir /ruta/instancia/data
```

Éste es el modo predeterminado. No crea archivos temporales ni modifica la base,
contenido, permisos o `mtime`; las lecturas pueden afectar `atime` del sistema de
archivos. Se puede guardar la salida JSON **fuera de los dos árboles**.

El informe contiene:

- `records`, `files`, `logical_bytes`: registros leídos, archivos examinados y suma
  de sus tamaños lógicos (incluye rutas que ya comparten inode).
- `candidates`, `duplicate_bytes`: rutas que podrían sustituirse y suma de sus
  tamaños lógicos; no son una garantía de espacio físico recuperado.
- `estimated_reclaimable_bytes`: estimación conservadora basada en bloques de los
  candidatos que tienen un solo enlace. Snapshots, CoW, compresión y otros enlaces
  pueden hacer que el espacio libre observado sea distinto.
- `already_linked`: rutas posteriores de cada grupo que ya comparten inode. No son
  candidatos nuevos ni suman `duplicate_bytes` o `estimated_reclaimable_bytes`: sus
  bytes físicos ya están compartidos.
- `linked`: reemplazos realmente realizados, siempre cero en dry-run.
- `skipped`, `skip_reasons`: problemas y archivos incompatibles omitidos.

Códigos de salida: `0` operación sin omisiones, `1` rechazo global/error de
preparación, `2` resultado parcial con omisiones (en apply puede haber enlaces ya
realizados). Leer siempre el informe. Cero candidatos no significa cero duplicados:
puede haber archivos con metadatos incompatibles, novelas distintas o ya enlazados.
No cambiar permisos automáticamente para forzar coincidencias. Los `mtime` diferentes
no impiden deduplicar; revisar el efecto sobre fechas antes de aplicar.

## 4. Aplicar explícitamente

Revisar el dry-run y el respaldo antes de ejecutar:

```bash
python3 tools/deduplicate-novel-images.py \
  --data-dir /ruta/instancia/data \
  --backup-dir /ruta/respaldos/yara-antes-dedup/data \
  --offline \
  --apply
```

Sin **ambos** `--apply` y `--offline` no se aplican enlaces. Con `--apply`, el respaldo
independiente completo es obligatorio y se verifica antes de cualquier reemplazo.
No hay opción de omitir esta comprobación. Los fallos de hardlinks (por ejemplo,
límites del filesystem o falta de permisos) se reportan sin copiar archivos como
fallback. Mantener el servicio detenido hasta revisar el resultado.

## 5. Verificar antes de reabrir el servicio

1. Repetir el comando de auditoría offline. Para los grupos compatibles, debe
   informar cero candidatos nuevos y aumentar `already_linked`; una segunda
   aplicación con el mismo respaldo también es idempotente mientras no haya
   cambiado el contenido del árbol. Investigar cualquier omisión.
2. Comprobar en una muestra de tres imágenes que las rutas siguen existiendo,
   sus tamaños y bytes no cambiaron y comparten dispositivo/inode; comparar
   la base y los archivos con la copia independiente. El borrado se valida en
   pruebas sintéticas, **no borrando capítulos reales como prueba**.
3. Verificar integridad de SQLite con una conexión de sólo lectura y sin writers.
   No interpretar el éxito de deduplicación como validación completa de SQLite.
4. Reiniciar únicamente la instancia detenida, reactivar sus workers/reinicio
   automático y abrir capítulos existentes con imágenes y URLs anteriores.
   Verificar que se sirven correctamente y que no hay errores en los logs.
5. Mantener el respaldo durante la ventana de recuperación definida por el operador.
   Una vez reiniciado el servicio, ese respaldo es histórico; no ejecutar apply
   contra él si hubo cambios en los datos. Una nueva intervención requiere parar,
   verificar y respaldar de nuevo.

La batería reproducible y aislada se ejecuta así:

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s tools -p 'test_deduplicate_novel_images.py' -v
```

Construye bases SQLite y tres archivos duplicados en directorios temporales. Cubre
modo por defecto sin escrituras, aplicación/idempotencia, URLs representadas por
rutas conservadas, eliminación de la ruta canónica sin perder las otras, aislamiento
entre novelas, comparación contra colisiones de hash, metadatos de acceso, timestamps
originalmente distintos (auditoría sin cambios y aplicación con `mtime` canónico),
hardlinks preexistentes sin bytes recuperables nuevos, `--data-dir` personalizado,
symlinks, archivos especiales, respaldo completo/independiente, reconocimiento offline,
WAL/journal y limpieza ante error de reemplazo. No arranca PocketBase ni prueba HTTP
real; esas comprobaciones operativas corresponden al paso anterior.

## 6. Rollback: restaurar la copia, no reconstruir enlaces

1. Si se reinició la instancia, detener otra vez todos los escritores y verificarlo
   como en el paso 1. Un rollback al respaldo pierde los cambios posteriores a su
   creación: preservarlos/investigarlos antes de restaurar, según el procedimiento
   de recuperación de la instalación.
2. Conservar el árbol remediado en cuarentena para diagnóstico, renombrándolo fuera
   de la ruta activa. No mezclar archivos sueltos de distintas fechas.
3. Restaurar **todo el directorio de datos** desde el respaldo independiente hacia
   una ruta activa que no exista. Usar una copia de bytes (`shutil.copytree` con
   `copy2`, como arriba), nunca hardlinks al árbol remediado ni al respaldo. De ese
   modo una futura escritura o eliminación no perjudica la copia de recuperación.
4. Restaurar permisos, propietario y claves externas de la instancia sin exponerlas.
   Comprobar integridad SQLite y archivos antes de arrancar. Arrancar el mismo
   servicio y verificar capítulos/imágenes y acceso a configuración cifrada.
5. No eliminar la cuarentena ni el respaldo hasta terminar la validación operativa.

No hay flag de rollback automático: la recuperación deliberadamente restaura una
copia offline completa y coherente de base, archivos y claves. El script nunca
reescribe filas para intentar deshacer enlaces.
