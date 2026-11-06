import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The PWA. Its built assets land directly in the Go embed directory, so
// `go build` embeds exactly what Vite produced. In dev, /api is proxied to the
// running app.
export default defineConfig({
    plugins: [svelte()],
    build: {
        outDir: '../internal/web/dist',
        emptyOutDir: true,
        target: 'es2022',
        sourcemap: false,
    },
    server: {
        port: 5174,
        strictPort: true,
        proxy: {
            '/api': 'http://127.0.0.1:9211',
        },
    },
});
