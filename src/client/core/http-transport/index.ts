/**
 * HTTP transport module entry point
 * 
 * Re-exports the main transport factory function to maintain
 * backward compatibility with existing imports.
 */

export { createSmartHttpLibp2pTransport } from "./transport-factory";
export * from "./cache-manager";
export * from "./resolver";
