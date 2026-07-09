# Migración PrimeVue → Naive UI

Rama: `migrate-to-naive-ui`

## Mapeo de componentes

| PrimeVue | Naive UI | Estado |
|----------|----------|--------|
| `Button` | `NButton` | ✅ |
| `InputText` | `NInput` | ✅ |
| `InputNumber` | `NInputNumber` | ✅ |
| `Textarea` | `NInput type="textarea"` | ✅ |
| `Password` | `NInput type="password"` | ✅ |
| `Select` | `NSelect` | ✅ |
| `SelectButton` | `NRadioGroup` + `NRadioButton` | ✅ |
| `RadioButton` | `NRadio` / `NRadioButton` | ✅ |
| `Checkbox` | `NCheckbox` | ✅ |
| `ToggleSwitch` | `NSwitch` | ✅ |
| `Card` | `NCard` | ✅ |
| `Dialog` | `NModal preset="card"` | ✅ |
| `Drawer` | `NDrawer` + `NDrawerContent` | ✅ |
| `Tag` | `NTag` | ✅ |
| `Message` | `NAlert` | ✅ |
| `Toast` | `NMessageProvider` + `useMessage()` | ✅ |
| `ConfirmDialog` | `useDialog()` + custom modal | ✅ |
| `ProgressBar` | `NProgress type="line"` | ✅ |
| `ProgressSpinner` | `NSpin` | ✅ |
| `Skeleton` | `NSkeleton` | ✅ |
| `DataTable/Column` | `NDataTable` | ✅ |
| `Menu` | `NDropdown` | ✅ |
| `Popover` | `NPopover` | ✅ (replaced with `useDialog()`) |
| `Accordion*` | `NCollapse` + `NCollapseItem` | ✅ |
| `useToast()` | `useMessage()` | ✅ |
| `useConfirm()` | `useDialog()` | ✅ |

## Fases

### Fase 0: Preparación
- [x] Instalar `naive-ui`, desinstalar `primevue`, `primeicons`, `@primeuix/themes`
- [x] Eliminar `src/theme/pixeo-preset.ts`
- [x] Crear `src/theme/naive-theme.ts`
- [x] Actualizar `src/main.ts`
- [x] Limpiar overrides CSS de PrimeVue en `styles.css`

### Fase 1: Componentes base
- [x] `FieldNumber.vue`
- [x] `LibrarySkeleton.vue`
- [x] `PromptRoleEditor.vue`
- [x] `NovelCard.vue`

### Fase 2: Componentes compartidos
- [x] `AppLayout.vue`
- [x] `JobsDrawer.vue`
- [x] `ChapterList.vue`
- [x] `MetadataEditor.vue`

### Fase 3: Páginas
- [x] `App.vue`
- [x] `LoginPage.vue`
- [x] `RegisterPage.vue`
- [x] `DashboardPage.vue`
- [x] `SettingsPage.vue`
- [x] `ChapterPage.vue`
- [x] `NovelDetailPage.vue`
- [x] `ReaderPage.vue`
- [x] `OperationsPage.vue`

### Fase 4: Feature components
- [x] `ImportUrlDialog.vue`
- [x] `ImportUrlConfirmDialog.vue`
- [x] `UpdateUrlDialog.vue`
- [x] `BulkImportDialog.vue`
- [x] `BatchTranslatePanel.vue`
- [x] `BatchUpdatePanel.vue`
- [x] `ProjectSettingsDialog.vue`

### Fase 5: Limpieza
- [x] Reemplazar `primeicons` con `@vicons/ionicons5`
- [x] Verificar dark mode (CSS overrides en `styles.css`)
- [x] `npm run build` sin errores de tipos
