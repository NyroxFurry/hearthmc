import { defineConfig, type Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const backend = process.env.HEARTH_BACKEND || 'http://127.0.0.1:18080';

// Lizenztexte aller eingebauten Bibliotheken und Schriften (erzeugt mit
// `make licenses`). Sie landen unter /licenses/ im Build und damit im Binary.
function thirdPartyLicenses(): Plugin {
  const file = resolve(import.meta.dirname, '../THIRD_PARTY_LICENSES.txt');
  return {
    name: 'hearth-third-party-licenses',
    apply: 'build',
    generateBundle() {
      if (!existsSync(file)) this.error('THIRD_PARTY_LICENSES.txt fehlt, bitte zuerst "make licenses" ausführen');
      this.emitFile({ type: 'asset', fileName: 'licenses/THIRD_PARTY_LICENSES.txt', source: readFileSync(file, 'utf8') });
    },
  };
}

export default defineConfig({
  plugins: [svelte(), thirdPartyLicenses()],
  build: {
    outDir: resolve(import.meta.dirname, '../internal/web/dist'),
    emptyOutDir: true,
    target: 'es2022',
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      input: {
        index: resolve(import.meta.dirname, 'index.html'),
        public: resolve(import.meta.dirname, 'public.html'),
      },
    },
  },
  server: {
    proxy: {
      '/api': { target: backend, ws: true, changeOrigin: false },
    },
  },
});
