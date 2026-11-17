import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
export default defineConfig({ plugins: [react()], server: { port: 18573, strictPort: true, proxy: { '/api': 'http://127.0.0.1:18083' } }, test: { environment: 'jsdom', setupFiles: './src/test-setup.ts', restoreMocks: true } });
