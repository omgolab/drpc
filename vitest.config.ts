import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.test.ts", "experiments/**/*.test.ts"],
    exclude: ["**/.trunk/**", "**/node_modules/**"],
    testTimeout: 30000,
  },
});
