import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['src/**/*.test.ts', 'src/**/*.integration.test.ts', 'examples/**/*.test.ts'],
    exclude: ['**/.trunk/**', '**/node_modules/**'],
    testTimeout: 30000,
  },
});