import { defineConfig } from "vitest/config";
import tsconfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
  plugins: [tsconfigPaths()],
  test: {
    include: ["src/**/*.test.ts", "experiments/**/*.test.ts"],
    exclude: ["**/.trunk/**", "**/node_modules/**"],
    testTimeout: 900000, // 15 minute timeout for all tests (including browser optimization)
    hookTimeout: 30000, // 30 second timeout for hooks (beforeAll, afterAll, etc.)
    // Browser testing configuration
    browser: {
      enabled: false, // Will be enabled per environment via CLI
      provider: 'playwright',
      headless: true,
    },
    pool: 'forks', // Use forks for better isolation
    poolOptions: {
      forks: {
        maxForks: 1, // Force integration tests to run sequentially
        minForks: 1,
      }
    },
    fileParallelism: false, // Run test files sequentially
    globals: true,
    environment: 'node', // Default to node environment
  },
  define: {
    global: 'globalThis',
  },
});
