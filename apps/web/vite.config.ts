import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

function getPackageName(id: string) {
  const normalizedId = id.replaceAll('\\', '/');
  const pnpmMatch = normalizedId.match(/node_modules\/\.pnpm\/[^/]+\/node_modules\/((?:@[^/]+\/)?[^/]+)/);
  if (pnpmMatch) return pnpmMatch[1];

  const nodeModulesMatch = normalizedId.match(/node_modules\/((?:@[^/]+\/)?[^/]+)/);
  return nodeModulesMatch?.[1];
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      '@relay-api/ui': path.resolve(__dirname, '../../packages/ui/src'),
      '@relay-api/lib': path.resolve(__dirname, '../../packages/lib/src'),
    },
  },
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          const packageName = getPackageName(id);
          if (!packageName) return undefined;
          if (['react', 'react-dom', 'scheduler', 'react-router', 'react-router-dom'].includes(packageName)) return 'react-vendor';
          if (packageName.startsWith('@radix-ui/')) return 'radix-vendor';
          if (packageName === 'framer-motion') return 'motion-vendor';
          if (packageName === 'lucide-react') return 'icons-vendor';
          if (['@tanstack/react-table', 'zustand', 'sonner'].includes(packageName)) return 'app-vendor';
          return 'vendor';
        },
      },
    },
  },
});
