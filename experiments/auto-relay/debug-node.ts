import { main } from "./debug";

// Node.js specific execution
await main().catch(console.error);
process.exit(0);
