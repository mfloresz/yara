# Dependencias de frontend: estado de seguridad

Verificado el 2026-11-18 con Node 22.22.2 y npm, contra el registro npm configurado.

## Correcciones aplicadas

- Se mantienen Vite 8 y `vite-plugin-pwa` 2, sin degradaciones ni desactivar la PWA.
- Override limitado a `workbox-build > glob` **14.0.6**. Workbox declara `glob ^11.0.1`, pero utiliza la API pública `globSync`, que sigue disponible. La generación de producción y de la PWA se ha probado con este override. La versión 14.0.6 corrige GHSA-3g3m-2fjr-6pw4 (recorrido que continuaba tras cancelar AbortSignal) y resuelve `minimatch` a **11.0.9**, fuera del rango afectado por GHSA-g3hv-75wg-m249. Retirar el override cuando Workbox incorpore una versión corregida por sí mismo, repitiendo build y auditoría.
- `serialize-javascript` ya resuelve a **7.1.4** a través de `@rollup/plugin-terser`; no necesita otro override ni una degradación a 7.1.3.

## Riesgo residual, no corregido

`npm audit` sigue devolviendo **12 avisos high** (salida no cero), todos derivados de **una vulnerabilidad raíz**: [GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf), ReDoS de `picomatch` con extglobs repetidos de una sola rama. `npm view picomatch version` devuelve **4.0.7**, incluida en el rango vulnerable `<=4.0.7`; no hay una versión corregida publicada según esta comprobación. Los otros once avisos son propagación por Vite, vue-tsc, tinyglobby/fdir, pluginutils/plugins de Rollup y Workbox/PWA, no once vulnerabilidades raíz adicionales.

No se ha añadido una excepción de auditoría, modificado la base de avisos, suprimido su salida ni usado `npm audit fix --force`. Las degradaciones sugeridas automáticamente no constituyen una solución: el aviso de picomatch afecta también versiones anteriores.

`npm audit --omit=dev` devuelve **0 vulnerabilidades**. Esto describe el árbol de dependencias de producción, no demuestra por sí solo que todos los artefactos generados sean seguros. Picomatch pertenece a herramientas Node de desarrollo/compilación, no a una dependencia de aplicación ejecutada por el navegador. El impacto residual es consumo excesivo de CPU/denegación de servicio en procesos que evalúen patrones maliciosos; no se afirma ejecución remota de código ni explotación comprobada en Yara.

### Mitigación mientras no exista parche

- Compilar únicamente checkouts, configuraciones y patrones glob confiables. No pasar patrones suministrados por usuarios a estas herramientas ni compilar código de terceros con secretos de producción.
- Ejecutar builds en trabajos aislados, con límites de CPU/tiempo y privilegios mínimos. Una compilación de código no confiable sigue siendo una frontera de seguridad aunque la aplicación se despliegue después como archivos estáticos.
- Servir en producción los artefactos estáticos generados, no el servidor de desarrollo/preview de Vite. `vite.config.ts` configura `host: true`: para desarrollo local usar `npm run dev -- --host 127.0.0.1`; no exponer ese servidor a redes no confiables. Esta es mitigación operativa documentada, no un control impuesto por el código.
- Repetir `npm audit` y `npm view picomatch version` al actualizar dependencias; incorporar la corrección upstream tan pronto exista y volver a probar build y PWA. Hasta entonces la auditoría completa no está limpia y la mitigación no elimina el defecto.

## Verificación reproducible

Desde `frontend/`:

```bash
npm ci --ignore-scripts
npm audit                 # Esperado actualmente: 12 high, salida no cero
npm audit --omit=dev      # Verificado: 0 vulnerabilidades
npm ls glob minimatch serialize-javascript picomatch
npm run build
```

Pasaron `npm ci --ignore-scripts` (lockfile reproducible), `npx vite build` y, tras ajustar la compatibilidad de tipos de ResetPasswordPage con vue-tsc actualizado, `npm run build` completo (`vue-tsc -b && vite build`). El build generó `dist/sw.js`, el runtime Workbox y un precache de 150 entradas; conserva el aviso no bloqueante de chunks mayores de 500 kB. No se ha probado la instalación/actualización offline en un navegador real como parte de este cambio de dependencias.
