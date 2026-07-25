import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { VitePWA } from "vite-plugin-pwa";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  server: {
    host: true,
    port: 5175,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:5176",
        changeOrigin: true,
      },
      "/ai": {
        target: "http://127.0.0.1:5176",
        changeOrigin: true,
      },
    },
  },
  plugins: [
    vue(),
    VitePWA({
      strategies: "generateSW",
      srcDir: "public",
      filename: "sw.js",
      includeAssets: ["favicon.svg", "favicon-32.png", "favicon-16.png", 
                     "android-chrome-192x192.png", "android-chrome-512x512.png",
                     "apple-touch-icon.png", "manifest.json"],
      manifest: {
        name: "Yara",
        short_name: "Yara",
        description: "Traductor de novelas literarias con IA",
        theme_color: "#1a1a2e",
        background_color: "#1a1a2e",
        display: "standalone",
        start_url: "/",
        icons: [
          {
            src: "/android-chrome-192x192.png",
            sizes: "192x192",
            type: "image/png",
          },
          {
            src: "/android-chrome-512x512.png",
            sizes: "512x512",
            type: "image/png",
          },
          {
            src: "/apple-touch-icon.png",
            sizes: "180x180",
            type: "image/png",
          },
          {
            src: "/favicon-32x32.png",
            sizes: "32x32",
            type: "image/png",
          },
          {
            src: "/favicon-16x16.png",
            sizes: "16x16",
            type: "image/png",
          },
        ],
      },
      workbox: {
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api\//],
        runtimeCaching: [
          {
            urlPattern: /\/api\/.*/,
            handler: "NetworkFirst",
            options: {
              cacheName: "api-cache",
              cacheableResponse: {
                statuses: [0, 200],
              },
            },
          },
          {
            urlPattern: /\.(js|css|png|jpg|jpeg|svg|woff2|woff|ttf|ico)$/,
            handler: "StaleWhileRevalidate",
            options: {
              cacheName: "assets-cache",
            },
          },
        ],
        globPatterns: ["**/*.{js,css,html,svg,png,ico,woff2,woff,ttf}"],
      },
    }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
});
