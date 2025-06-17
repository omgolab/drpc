#!/usr/bin/env tsx
/**
 * dRPC Integration Test Orchestrator
 * 
 * Usage:
 *   tsx src/integration-test/orchestrator.ts                 # All environments (sequential)
 *   tsx src/integration-test/orchestrator.ts --env=node      # Node.js only
 *   tsx src/integration-test/orchestrator.ts --env=chrome    # Chrome only
 *   tsx src/integration-test/orchestrator.ts --env=firefox   # Firefox only
 */

import { spawn } from 'child_process';
import { getUtilServer, isUtilServerAccessible } from '../util/util-server';

const VALID_ENVIRONMENTS = ['node', 'chrome', 'firefox'];

function parseEnvironments(): string[] {
    const args = process.argv.slice(2);

    if (args.includes('--help') || args.includes('-h')) {
        console.log(`
🧪 dRPC Integration Test Orchestrator

USAGE: tsx src/integration-test/orchestrator.ts [OPTIONS]

OPTIONS:
--env=<environment>    Run specific environment only

ENVIRONMENTS: ${VALID_ENVIRONMENTS.join(', ')}
        `);
        process.exit(0);
    }

    const envArg = args.find(arg => arg.startsWith('--env='));
    const targetEnv = envArg?.split('=')[1] || process.env.TEST_ENV;

    if (targetEnv) {
        if (!VALID_ENVIRONMENTS.includes(targetEnv)) {
            console.error(`❌ Invalid environment: ${targetEnv}. Valid: ${VALID_ENVIRONMENTS.join(', ')}`);
            process.exit(1);
        }
        return [targetEnv];
    }

    return VALID_ENVIRONMENTS;
}

async function runEnvironment(env: string): Promise<boolean> {
    console.log(`\n${'='.repeat(60)}`);
    console.log(`🧪 Running dRPC Integration Tests in ${env.toUpperCase()}`);
    console.log(`${'='.repeat(60)}`);

    // Build vitest command
    const args = [
        'vitest', 'run',
        'src/integration-test/drpc-client.integration.test.ts',
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
        const vitestProcess = spawn('npx', args, {
            stdio: 'inherit',
            env: { ...process.env, TEST_ENV: env }
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

async function main() {
    const environments = parseEnvironments();
    const isAll = environments.length > 1;

    console.log(`🚀 dRPC Integration Test Orchestrator`);
    console.log(`🎯 Running: ${isAll ? 'ALL' : environments[0].toUpperCase()}`);
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
            const passed = await runEnvironment(env);
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

        process.exit(allPassed ? 0 : 1);

    } catch (error) {
        console.error('❌ Error:', error);
        process.exit(1);
    }
}

main().catch(error => {
    console.error('❌ Fatal error:', error);
    process.exit(1);
});
