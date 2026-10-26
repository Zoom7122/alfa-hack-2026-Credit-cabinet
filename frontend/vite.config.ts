import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';

// Демо-запросы /api перехватывает MSW (src/mocks); при VITE_API_MOCK=false фронт их не делает.
export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, process.cwd(), '');
    return {
        plugins: [react()],
        server: {
            proxy: {
                '/api': { target: env.API_PROXY_TARGET || 'http://localhost:8080', changeOrigin: true, ws: true },
                // data-collector: без моков (VITE_COLLECTOR_MOCK=false) — в живой сервис, префикс /collector срезаем
                '/collector': {
                    target: env.COLLECTOR_PROXY_TARGET || 'http://localhost:8081',
                    changeOrigin: true,
                    rewrite: (path) => path.replace(/^\/collector/, ''),
                },
            },
        },
    };
});
