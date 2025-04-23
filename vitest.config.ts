import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['src/**/*.test.ts', 'src/**/*.integration.test.ts'],
    exclude: ['**/.trunk/**', '**/node_modules/**'],
  },
});