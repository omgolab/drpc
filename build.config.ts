#!/usr/bin/env bun

/**
 * Production build script with guaranteed logger elimination for dRPC
 * 
 * This script uses Bun's native bundler with the `drop` option for initial cleanup,
 * then applies post-processing to remove any remaining logger calls that weren't 
 * caught due to variable minification.
 */
import { readFileSync, writeFileSync } from 'fs';
import { join } from 'path';

async function build() {
    console.log('🚀 Building production bundle...');

    // Build with Bun's native drop option for logger calls
    // Note: drop option works best before minification, but minified variable names
    // may still contain logger calls that need post-processing
    const buildResult = await Bun.build({
        entrypoints: ['src/client/index.ts'],
        outdir: 'dist',
        minify: true,
        target: 'browser',
        splitting: true,
        format: 'esm',
        drop: [
            'console',     // Remove console.log, console.debug, etc.
            'debugger',    // Remove debugger statements
            'logger.debug', // Remove logger.debug calls (before minification)
            'logger.info',  // Remove logger.info calls (before minification) 
            'log.debug',   // Also catch any 'log' variable patterns
            'log.info',
        ],
        define: {
            'process.env.NODE_ENV': '"production"',
        }
    });

    if (!buildResult.success) {
        console.error('❌ Build failed:', buildResult.logs);
        process.exit(1);
    }

    console.log('✅ Initial build completed successfully!');

    // Post-process to remove any remaining logger calls after minification
    const distPath = join(process.cwd(), 'dist', 'index.js');
    const content = readFileSync(distPath, 'utf-8');

    // Pattern to match any variable name followed by .debug, .info calls
    const loggerPattern = /[a-zA-Z_$][a-zA-Z0-9_$]*\.(debug|info)\(/g;
    const matches = content.match(loggerPattern);

    if (matches && matches.length > 0) {
        console.log(`🔧 Found ${matches.length} logger calls remaining after Bun drop option`);
        console.log('   This is expected due to variable name minification');
        console.log('🧹 Running post-processing to remove remaining logger calls...');

        let processedContent = content;

        // Remove logger calls with comprehensive patterns
        // Handle normal parentheses with various argument patterns
        processedContent = processedContent.replace(/[a-zA-Z_$][a-zA-Z0-9_$]*\.debug\([^)]*\);?/g, '');
        processedContent = processedContent.replace(/[a-zA-Z_$][a-zA-Z0-9_$]*\.info\([^)]*\);?/g, '');

        // Handle template literals and complex nested expressions
        processedContent = processedContent.replace(/[a-zA-Z_$][a-zA-Z0-9_$]*\.debug\(`[^`]*`\);?/g, '');
        processedContent = processedContent.replace(/[a-zA-Z_$][a-zA-Z0-9_$]*\.info\(`[^`]*`\);?/g, '');

        // Clean up syntax issues that might be introduced
        processedContent = processedContent.replace(/,,+/g, ',');        // Multiple commas
        processedContent = processedContent.replace(/;\s*;/g, ';');       // Double semicolons
        processedContent = processedContent.replace(/\(\s*,/g, '(');      // Leading commas in function calls
        processedContent = processedContent.replace(/,\s*\)/g, ')');      // Trailing commas in function calls

        writeFileSync(distPath, processedContent);
        console.log('✅ Post-processing completed successfully!');
    } else {
        console.log('🎉 Perfect! No logger calls found - Bun drop option worked completely!');
    }

    // Final verification step
    const finalContent = readFileSync(distPath, 'utf-8');
    const finalMatches = finalContent.match(loggerPattern);

    if (finalMatches && finalMatches.length > 0) {
        console.error(`❌ Warning: ${finalMatches.length} logger calls still remain in the bundle:`);
        console.error(finalMatches.slice(0, 5)); // Show first 5 for debugging
        console.error('Build completed but may still contain unwanted logger calls.');
        process.exit(1);
    } else {
        console.log('✅ Final verification: Production bundle is completely clean!');
        console.log('   All logger.debug and logger.info calls have been removed.');
    }

    // Show bundle info
    const stats = require('fs').statSync(distPath);
    console.log(`📦 Bundle size: ${(stats.size / 1024).toFixed(2)} KB`);
}

build().catch(console.error);
