/**
 * Browser Test Setup - injects debug patterns into localStorage
 * 
 * This file is loaded before browser tests run and sets up debug logging
 * based on environment variables passed from menv-runner.
 */

// Declare global variable injected by vitest
declare const __VITEST_BROWSER_DEBUG__: string;

// Only run in browser environment
if (typeof window !== 'undefined' && typeof localStorage !== 'undefined') {
  // Check for debug pattern from vitest environment
  const debugPattern = __VITEST_BROWSER_DEBUG__;
  
  if (debugPattern) {
    console.log(`🐛 Setting up browser debug logging: ${debugPattern}`);
    localStorage.setItem('debug', debugPattern);
    
    // Also set up the debug library for immediate use
    try {
      // Enable debug logging via the debug library
      const debug = require('debug');
      debug.enabled = () => true;
      debug.log = console.log.bind(console);
    } catch (error) {
      // If debug library is not available, that's okay
      console.log('Debug library not available, using localStorage only');
    }
  } else {
    // Clean up any existing debug pattern if not explicitly set
    localStorage.removeItem('debug');
  }
}
