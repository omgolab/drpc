import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.test.ts", "experiments/**/*.test.ts"],
    exclude: ["**/.trunk/**", "**/node_modules/**"],
    testTimeout: 900000, // 15 minute timeout for all tests (including browser optimization)
    // Browser testing configuration
    browser: {
      enabled: false, // Will be enabled per environment via CLI
      provider: 'playwright',
      headless: true,
    },
    pool: 'forks', // Use forks for better isolation
    globals: true,
    environment: 'node', // Default to node environment
  },
  define: {
    global: 'globalThis',
  },
});
