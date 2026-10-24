import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';

// Без бэкенда запросы /api перехватывает MSW (src/mocks). С бэкендом (VITE_API_MOCK=false)
// dev-сервер проксирует /api на API_PROXY_TARGET.
export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, process.cwd(), '');
    return {
        plugins: [react()],
        server: {
            proxy: {
                '/api': { target: env.API_PROXY_TARGET || 'http://localhost:8080', changeOrigin: true, ws: true },
            },
        },
    };
});
