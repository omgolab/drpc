#!/usr/bin/env tsx
/**
 * Multi-Environment Test Runner Utility
 * 
 * Usage:
 *   tsx src/tests/util/menv-runner.ts --file=path/to/test.ts                 # All environments (sequential)
 *   tsx src/tests/util/menv-runner.ts --file=path/to/test.ts --env=node      # Node.js only
 *   tsx src/tests/util/menv-runner.ts --file=path/to/test.ts --env=chrome    # Chrome only
 *   tsx src/tests/util/menv-runner.ts --file=path/to/test.ts --env=firefox   # Firefox only
 */

import { spawn } from 'child_process';
import { getUtilServer, isUtilServerAccessible } from './util-server';

const VALID_ENVIRONMENTS = ['node', 'chrome', 'firefox'];

function parseArguments(): { file: string; environments: string[]; debug?: string } {
    const args = process.argv.slice(2);

    if (args.includes('--help') || args.includes('-h')) {
        console.log(`
🧪 Multi-Environment Test Runner

USAGE: tsx src/tests/util/menv-runner.ts --file=<test-file> [OPTIONS]

REQUIRED:
--file=<path>          Path to the test file to run

OPTIONS:
--env=<environment>    Run specific environment only
--debug=<pattern>      Enable debug logging (e.g., --debug=* or --debug=libp2p:*)

ENVIRONMENTS: ${VALID_ENVIRONMENTS.join(', ')}

DEBUG EXAMPLES:
--debug=*              Enable all debug logs
--debug=libp2p:*       Enable all libp2p logs
--debug=libp2p:mdns*   Enable mDNS logs only (Node.js only)
--debug=libp2p:dht*    Enable DHT logs only
        `);
        process.exit(0);
    }

    const fileArg = args.find(arg => arg.startsWith('--file='));
    const file = fileArg?.split('=')[1];

    if (!file) {
        console.error('❌ Missing required --file argument');
        console.error('Usage: tsx src/tests/util/menv-runner.ts --file=path/to/test.ts');
        process.exit(1);
    }

    const envArg = args.find(arg => arg.startsWith('--env='));
    const targetEnv = envArg?.split('=')[1] || process.env.TEST_ENV;

    const debugArg = args.find(arg => arg.startsWith('--debug='));
    const debug = debugArg?.split('=')[1];

    let environments: string[];
    if (targetEnv) {
        if (!VALID_ENVIRONMENTS.includes(targetEnv)) {
            console.error(`❌ Invalid environment: ${targetEnv}. Valid: ${VALID_ENVIRONMENTS.join(', ')}`);
            process.exit(1);
        }
        environments = [targetEnv];
    } else {
        environments = VALID_ENVIRONMENTS;
    }

    return { file, environments, debug };
}

async function runEnvironment(file: string, env: string, debug?: string): Promise<boolean> {
    console.log(`\n${'='.repeat(60)}`);
    console.log(`🧪 Running tests in ${env.toUpperCase()}: ${file}`);
    if (debug) {
        console.log(`🐛 Debug enabled: ${debug}`);
        if (env === 'node') {
            console.log(`   └─ Node.js: Using DEBUG environment variable`);
        } else {
            console.log(`   └─ Browser: Injected via vitest config define and localStorage`);
        }
    }
    console.log(`${'='.repeat(60)}`);

    // Build vitest command
    const args = [
        'vitest', 'run',
        file,
        '--reporter=verbose',
        '--no-watch'
    ];

    if (env === 'node') {
        args.push('--environment=node');
    } else {
        args.push('--browser.enabled');
        args.push(`--browser.name=${env === 'chrome' ? 'chromium' : 'firefox'}`);
        args.push('--browser.headless');
        args.push('--browser.provider=playwright');
    }

    console.log(`🔧 Command: npx ${args.join(' ')}`);

    return new Promise<boolean>((resolve) => {
        const processEnv: Record<string, string | undefined> = { ...process.env, TEST_ENV: env };

        // For Node.js: Add DEBUG environment variable
        if (debug && env === 'node') {
            processEnv.DEBUG = debug;
        }

        // For browsers: Pass debug pattern via environment variable for vitest config
        if (debug && env !== 'node') {
            processEnv.VITEST_BROWSER_DEBUG = debug;
        }

        const vitestProcess = spawn('npx', args, {
            stdio: 'inherit',
            env: processEnv
        });

        vitestProcess.on('close', (code) => {
            const success = code === 0;
            console.log(`${success ? '✅' : '❌'} ${env} tests ${success ? 'passed' : 'failed'}`);
            resolve(success);
        });

        vitestProcess.on('error', (error) => {
            console.error(`❌ Failed to start ${env} tests:`, error);
            resolve(false);
        });
    });
}

export async function runMultiEnvironmentTests(testFile: string, targetEnvironments?: string[], debug?: string): Promise<boolean> {
    const environments = targetEnvironments || VALID_ENVIRONMENTS;
    const isAll = environments.length > 1;

    console.log(`🚀 Multi-Environment Test Runner`);
    console.log(`📁 Test File: ${testFile}`);
    console.log(`🎯 Running: ${isAll ? 'ALL' : environments[0].toUpperCase()}`);
    if (debug) {
        console.log(`🐛 Debug Pattern: ${debug}`);
    }
    console.log(`⚡ Mode: SEQUENTIAL`);

    try {
        // Start util server
        const server = getUtilServer();
        console.log("🔄 Starting utility server...");
        await server.startServer();

        // Wait for server to be ready
        let retries = 5;
        while (retries > 0 && !(await isUtilServerAccessible())) {
            console.log(`⏳ Waiting for server... (${retries} retries left)`);
            await new Promise(resolve => setTimeout(resolve, 2000));
            retries--;
        }

        if (!(await isUtilServerAccessible())) {
            throw new Error("❌ Utility server failed to start");
        }

        const nodeInfo = await server.getPublicNodeInfo();
        console.log(`✅ Utility server ready at ${nodeInfo.http_address}`);

        // Run tests sequentially
        let allPassed = true;
        const results: Record<string, boolean> = {};

        for (const env of environments) {
            const passed = await runEnvironment(testFile, env, debug);
            results[env] = passed;
            allPassed = allPassed && passed;
        }

        // Summary
        console.log(`\n${'='.repeat(40)}`);
        console.log('📊 RESULTS');
        console.log(`${'='.repeat(40)}`);
        Object.entries(results).forEach(([env, passed]) => {
            console.log(`${env}: ${passed ? '✅ PASSED' : '❌ FAILED'}`);
        });

        console.log(`\n${allPassed ? '🎉 ALL TESTS PASSED' : '💥 SOME TESTS FAILED'}`);

        // Cleanup
        console.log("🛑 Shutting down...");
        await server.stopServer();
        await server.cleanupOrphanedProcesses();

        return allPassed;

    } catch (error) {
        console.error('❌ Error:', error);
        return false;
    }
}

// Generic menv wrapper utility for creating specific test runners
export function createMenvWrapper(testFile: string, title: string) {
    const VALID_ENVIRONMENTS = ['node', 'chrome', 'firefox'];

    function parseEnvironments(): { environments: string[]; debug?: string } {
        const args = process.argv.slice(2);

        if (args.includes('--help') || args.includes('-h')) {
            console.log(`
🧪 ${title}

USAGE: [OPTIONS]

OPTIONS:
--env=<environment>    Run specific environment only
--debug=<pattern>      Enable debug logging (e.g., --debug=* or --debug=libp2p:*)

ENVIRONMENTS: ${VALID_ENVIRONMENTS.join(', ')}

DEBUG EXAMPLES:
--debug=*              Enable all debug logs
--debug=libp2p:*       Enable all libp2p logs
--debug=libp2p:mdns*   Enable mDNS logs only (Node.js only)
--debug=libp2p:dht*    Enable DHT logs only
            `);
            process.exit(0);
        }

        const envArg = args.find(arg => arg.startsWith('--env='));
        const targetEnv = envArg?.split('=')[1] || process.env.TEST_ENV;

        const debugArg = args.find(arg => arg.startsWith('--debug='));
        const debug = debugArg?.split('=')[1];

        let environments: string[];
        if (targetEnv) {
            if (!VALID_ENVIRONMENTS.includes(targetEnv)) {
                console.error(`❌ Invalid environment: ${targetEnv}. Valid: ${VALID_ENVIRONMENTS.join(', ')}`);
                process.exit(1);
            }
            environments = [targetEnv];
        } else {
            environments = VALID_ENVIRONMENTS;
        }

        return { environments, debug };
    }

    return {
        async run() {
            const { environments, debug } = parseEnvironments();

            console.log(`🚀 ${title}`);
            console.log(`📁 Test File: ${testFile}`);

            const success = await runMultiEnvironmentTests(testFile, environments, debug);
            process.exit(success ? 0 : 1);
        }
    };
}

// CLI entry point
async function main() {
    const { file, environments, debug } = parseArguments();

    const success = await runMultiEnvironmentTests(file, environments, debug);
    process.exit(success ? 0 : 1);
}

// Only run main if this file is executed directly
if (import.meta.url === `file://${process.argv[1]}`) {
    main().catch(error => {
        console.error('❌ Fatal error:', error);
        process.exit(1);
    });
}
