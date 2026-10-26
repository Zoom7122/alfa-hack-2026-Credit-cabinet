import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({plugins:[react()],build:{outDir:'../internal/adminui/dist',emptyOutDir:true},server:{port:5184,proxy:{'/api':{target:process.env.ADMIN_PROXY_TARGET||'http://127.0.0.1:8084',changeOrigin:true}}}});
