<template>
  <!--
    Passthrough since the shell refactor: the persistent
    AppShell (see app/App.vue) owns the sidebar, the header and the scrolling
    main area. Pages keep wrapping their content in <AppLayout> so their
    templates did not have to change; immersive pages (reader) bypass it
    entirely via route meta.
  -->
  <slot />
</template>
