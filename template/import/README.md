# Import Template

Estructura esperada para importar una novela desde un ZIP con
`POST /api/v1/novels/import-zip`.

## Formato

```
novela.zip
├── metadata.json              # Metadatos de la novela (obligatorio)
├── cover.jpg                  # Portada (opcional: .jpg/.jpeg/.png/.gif/.webp)
├── images/                    # Imágenes inline de los capítulos (opcional)
│   ├── mapa.jpg
│   └── esquema.png
├── originals/                 # Capítulos originales (obligatorio)
│   ├── Capítulo 001.txt       # .txt o .md
│   ├── Capítulo 002.txt
│   └── ...
└── translated/                # Capítulos traducidos (opcional)
    ├── Capítulo 001.txt       # Mismo nombre que en originals/
    ├── Capítulo 002.txt
    └── ...
```

El ZIP puede ir envuelto en una carpeta raíz (`novela.zip/novela/metadata.json`):
si todos los archivos comparten el mismo prefijo, se detecta y se descarta solo.
Los nombres de archivo se comparan **sin distinguir mayúsculas**, y una carpeta
extra (por ejemplo `originals/` con subcarpetas) se ignora.

## Reglas

### Capítulos

- Cada archivo en `originals/` se convierte en un capítulo con `originalContent`.
- Si existe un archivo con el **mismo nombre** en `translated/` y no está vacío
  (los placeholders de 0 bytes se descartan), ese capítulo queda con estado
  `translated` y su contenido en `translatedContent` + `translatedTitle`.
- Si no existe en `translated/`, el capítulo queda `pending`.
- El **orden** se extrae del nombre del archivo (primer número encontrado). Los
  archivos sin número caen al final en orden alfabético y reciben el índice
  denso (`1..n`).
- El **título** se extrae de la primera línea del archivo (se quitan marcadores
  `#` y el formato markdown: `**`, `__`, `*`, `~~`, `` ` ``). Si el archivo está
  vacío, se usa el nombre del archivo como título.
- La primera línea se consume como título: el contenido guardado es el resto del
  archivo.

### Imágenes inline

Cualquier archivo con extensión `.jpg`, `.jpeg`, `.png`, `.gif`, `.webp` o
`.svg` en el ZIP se considera una imagen inline (en cualquier carpeta; se
identifica por su **nombre de archivo base**, y si hay duplicados gana el
primero encontrado). La portada `cover.*` se cuenta aparte.

Para insertar una imagen en el texto de un capítulo usa un marcador
`[[IMG:nombre.jpg]]` en `originals/`:

```
Capítulo 001.txt
────────────────
El mapa estaba en [[IMG:mapa.jpg]] cuando llegamos al valle.
```

Reglas de los marcadores:

- `originals/` es la fuente canónica: cada marcador distinto recibe el token
  `[[IMG-n]]` **en orden de primera aparición** dentro del capítulo (n empieza en 1).
- `translated/` se reescribe con el **mismo** mapeo, así que en la traducción
  también hay que escribir `[[IMG:mapa.jpg]]` (no `[[IMG-1]]`).
- Un marcador de la traducción que no existe en el original se descarta
  silenciosamente.
- Un marcador que referencia una imagen **inexistente en el ZIP** es un error de
  autoría: la importación falla con `400`.
- Límites: 20 MB por imagen y 64 MB en total para todas las imágenes. El texto
  (resto de entradas) tiene su propio presupuesto de 25 MB descomprimidos
  (protección zip-bomb) y el cuerpo multipart admite hasta 256 MB.

Una vez importada, la imagen queda inmutable y se sirve con autenticación en
`GET /api/v1/novels/{novelId}/images/{imageId}` (permisos de la novela:
propietario o `isPublic`). El contenido del capítulo trae los `[[IMG-n]]` y, en
las respuestas con contenido completo (`?includeContent=true` o el detalle de un
capítulo), un array `images` con `id`, `chapterId`, `token`, `num`, `alt`, `mime`
y `url`.

### Portada

`cover.jpg` / `cover.png` / `cover.gif` / `cover.webp` en la raíz del ZIP (o en
la carpeta raíz detectada). `.svg` no se acepta como portada. También puedes
subirla después con `POST /api/v1/novels/{id}/cover` (`multipart/form-data`,
campo `cover`).

## metadata.json

```json
{
  "sourceTitle": "Título original",
  "sourceAuthor": "Autor original",
  "sourceDescription": "Descripción original",
  "sourceLanguage": "en",
  "targetLanguage": "es",
  "url": "https://novelfire.net/novel/xxxxx",
  "targetTitle": "",
  "targetAuthor": "",
  "targetDescription": "",
  "sourceSeries": "",
  "sourceNumber": "",
  "targetSeries": "",
  "targetNumber": "",
  "notes": "",
  "customCommands": "",
  "status": "completed",
  "isPublic": false
}
```

- Obligatorios: `sourceTitle`, `sourceLanguage` y `targetLanguage`.
- `status`: `ongoing` (por defecto), `completed`, `hiatus`, `cancelled`.
  Cualquier otro valor se guarda como `ongoing`.
- Si `metadata.json` no es JSON válido, la importación falla con `400`.
- Claves presentes en la plantilla que **la importación ZIP no lee**:
  `glossary`, `prompts`, `aiOptions`, `translationOptions`, `cleanupRules` (se
  crean vacías). Configúralas después con `PATCH /api/v1/novels/{id}`.
- El `title` no es una clave reconocida: usa `sourceTitle` (y `targetTitle` para el
  título ya traducido).

### Idiomas

`sourceLanguage` y `targetLanguage` se guardan siempre en minúsculas y sin espacios
(`"ES"` se almacena como `"es"`), así que no importa cómo los escribas.

Si ambos valores son iguales, la novela se marca como **sin necesidad de traducción**:
sus capítulos pueden quedarse en `pending` y aun así aparecerá en el filtro
*Completamente traducidas* de la biblioteca, porque no hay nada que traducir.

## Cómo importar

### 1. Crear el ZIP

```bash
cd /ruta/a/tu/novela
zip -r novela.zip metadata.json cover.jpg images/ originals/ translated/
```

### 2. Autenticarse

El login entrega el token **solo como cookie `HttpOnly`** (`auth.token`), nunca en
el cuerpo de la respuesta. Con `curl`, usa un cookie jar:

```bash
# ¿La instalación todavía no tiene usuarios? (el primero se registra como admin)
curl -s http://localhost:8090/api/v1/auth/setup-status
# → { "data": { "needsSetup": true } }

# Primer usuario (solo en instalaciones vacías). El registro posterior exige invitación.
curl -c cookies.txt -X POST http://localhost:8090/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"tu@email.com","password":"tu_password","name":"Tu Nombre"}'

# Login normal
curl -c cookies.txt -X POST http://localhost:8090/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"tu@email.com","password":"tu_password"}'
```

### 3. Importar

```bash
curl -b cookies.txt -X POST http://localhost:8090/api/v1/novels/import-zip \
  -F "file=@novela.zip"
```

Respuesta `201 Created` (con cabecera `Location: /api/v1/novels/{id}`):

```json
{
  "data": {
    "novel": { "id": "...", "sourceTitle": "...", "chapterCount": 2, "...": "..." },
    "chaptersImported": 2
  }
}
```

Errores frecuentes (`400`): falta `metadata.json`, falta `originals/`,
`metadata.json` inválido, un `[[IMG:...]]` sin imagen correspondiente en el ZIP, o los
límites de tamaño anteriores.

### 4. Actualizar desde URL (si la novela tiene URL)

Si `metadata.json` incluye `"url"`, puedes descargar capítulos nuevos más tarde:

```bash
# Ver cuántos capítulos nuevos hay
curl -b cookies.txt -X POST http://localhost:8090/api/v1/novels/{novel_id}/check-preview

# Descargar nuevos capítulos (202 Accepted; encola un job)
curl -b cookies.txt -X POST http://localhost:8090/api/v1/novels/{novel_id}/update-from-url \
  -H "Content-Type: application/json" \
  -d '{}'
```

`update-from-url` acepta `startChapter` / `endChapter` (`0` = todos) y devuelve
`409` si la novela ya tiene un job `pending` o `running`.

## Ver los capítulos y sus imágenes

```bash
# Resúmenes (sin contenido): opción por defecto
curl -b cookies.txt "http://localhost:8090/api/v1/novels/$NOVEL_ID/chapters"

# Contenido completo + array images
curl -b cookies.txt "http://localhost:8090/api/v1/novels/$NOVEL_ID/chapters?includeContent=true"

# Bytes de una imagen (cookie o token)
curl -b cookies.txt "http://localhost:8090/api/v1/novels/$NOVEL_ID/images/$IMAGE_ID"
```

## Alternativas a la importación por ZIP

- **EPUB**: `POST /api/v1/novels/import-epub` con `multipart/form-data`
  (`file`, `sourceLanguage`, `targetLanguage`; el idioma origen puede omitirse si
  el EPUB lo declara). Trae portada, portada interna, imágenes inline y metadatos
  del EPUB automáticamente. Devuelve `201` con `data.novel`, `data.epub` y
  `data.chaptersImported`.
- **Desde URL**: `POST /api/v1/novels/preview-from-url` (metadatos + lista de
  capítulos sin crear nada) y luego `POST /api/v1/novels/import-from-url`
  (`url`, `sourceLanguage`, `targetLanguage`, `startChapter`, `endChapter`),
  que importa el primer capítulo de forma síncrona y encola el resto.
