/**
 * Peer Discovery Parameter Optimization Tool (Vitest Version)
 * 
 * This tool performs adaptive optimization to find the optimal parameter combinations
 * for peer discovery in libp2p networks using ambient relay discovery.
 * 
 * **Key Features:**
 * - Adaptive Grid Search with Progressive Refinement algorithm
 * - Tests multiple combinations of connectIntervalMs and dialTimeoutMs
 * - Uses intelligent search space refinement to converge on optimal values
 * - Provides statistical analysis including success rates and geometric means
 * - Exports results for use in production code
 * - Browser and Node.js environment compatibility through Vitest
 * 
 * **Algorithm:**
 * 1. Start with coarse grid over parameter space
 * 2. Test candidates and find best performing region
 * 3. Zoom into best region and refine search
 * 4. Repeat until convergence or max iterations
 * 
 * @fileoverview Adaptive parameter optimization for peer discovery performance (Vitest compatible)
 */

import { describe, test, expect } from 'vitest';
import { type Libp2p } from 'libp2p';
import { discoverOptimalConnectPath } from '../../src/client/core/discover.js';
import { createLibp2pHost } from '../../src/client/core/libp2p-host.js';

/**
 * Optimization candidate representing a tested parameter combination
 */
interface OptimizationCandidate {
    /** Connection retry interval in milliseconds */
    connectIntervalMs: number;
    /** Individual dial timeout in milliseconds */
    dialTimeoutMs: number;
    /** Combined performance score (success_rate * 100 - geometric_mean) */
    score: number;
    /** Percentage of successful connection attempts (0-1) */
    successRate: number;
    /** Geometric mean of successful connection times in seconds */
    geometricMean: number;
    /** Total number of test attempts for this candidate */
    attempts: number;
    /** Array of successful connection times in seconds */
    successfulTimes: number[];
    /** Number of failed connection attempts */
    failedAttempts: number;
}

/**
 * Defines the parameter search space boundaries for optimization
 */
interface SearchSpace {
    /** Minimum connection retry interval in milliseconds */
    connectIntervalMin: number;
    /** Maximum connection retry interval in milliseconds */
    connectIntervalMax: number;
    /** Minimum dial timeout in milliseconds */
    dialTimeoutMin: number;
    /** Maximum dial timeout in milliseconds */
    dialTimeoutMax: number;
}

interface OptimizationConfig {
    // Initial search space
    initialSpace: SearchSpace;
    // Step sizes for sampling
    stepSize: number;
    // Number of samples per dimension in each iteration
    samplesPerDimension: number;
    // Number of attempts per candidate
    attemptsPerCandidate: number;
    // Maximum iterations
    maxIterations: number;
    // Connection timeout
    timeoutMs: number;
    // Cooldown between tests
    cooldownMs: number;
    // Convergence threshold (when to stop)
    convergenceThreshold: number;
}

const DEFAULT_CONFIG: OptimizationConfig = {
    initialSpace: {
        connectIntervalMin: 50,
        connectIntervalMax: 3000,
        dialTimeoutMin: 50,
        dialTimeoutMax: 3000
    },
    stepSize: 50,
    samplesPerDimension: 6, // 6x6 = 36 candidates per iteration
    attemptsPerCandidate: 5, // More attempts for better statistical significance
    maxIterations: 6, // Should converge quickly with adaptive refinement
    timeoutMs: 30000,
    cooldownMs: 500,
    convergenceThreshold: 0.1 // Stop when search space is < 10% of original
};

const FAST_CONFIG: OptimizationConfig = {
    initialSpace: {
        connectIntervalMin: 100,
        connectIntervalMax: 500,
        dialTimeoutMin: 1000,
        dialTimeoutMax: 2000
    },
    stepSize: 200,
    samplesPerDimension: 2, // 2x2 = 4 candidates per iteration
    attemptsPerCandidate: 2, // Reduced for faster testing
    maxIterations: 2,
    timeoutMs: 15000,
    cooldownMs: 200,
    convergenceThreshold: 0.3
};

const BROWSER_CONFIG: OptimizationConfig = {
    initialSpace: {
        connectIntervalMin: 1000,
        connectIntervalMax: 2000,
        dialTimeoutMin: 10000,
        dialTimeoutMax: 20000
    },
    stepSize: 1000,
    samplesPerDimension: 2, // Very reduced for browsers
    attemptsPerCandidate: 1, // Single attempt for browsers
    maxIterations: 1,
    timeoutMs: 400000, // 6.67 minutes for browser connections 
    cooldownMs: 1000,
    convergenceThreshold: 0.5
};

class AdaptiveOptimizer {
    private config: OptimizationConfig;
    private targetPeerId: string = '';
    private allResults: OptimizationCandidate[] = [];

    constructor(config: OptimizationConfig = DEFAULT_CONFIG) {
        this.config = config;
    }

    private calculateGeometricMean(values: number[]): number {
        if (values.length === 0) return Infinity;
        const product = values.reduce((acc, val) => acc * val, 1);
        return Math.pow(product, 1 / values.length);
    }

    private calculateSpaceSize(space: SearchSpace): number {
        return (space.connectIntervalMax - space.connectIntervalMin) *
            (space.dialTimeoutMax - space.dialTimeoutMin);
    }

    private generateCandidates(space: SearchSpace): Array<{ connectIntervalMs: number, dialTimeoutMs: number }> {
        const candidates: Array<{ connectIntervalMs: number, dialTimeoutMs: number }> = [];

        // Generate evenly spaced samples in each dimension
        const intervalStep = Math.max(this.config.stepSize, (space.connectIntervalMax - space.connectIntervalMin) / (this.config.samplesPerDimension - 1));
        const dialTimeoutStep = Math.max(this.config.stepSize, (space.dialTimeoutMax - space.dialTimeoutMin) / (this.config.samplesPerDimension - 1));

        for (let i = 0; i < this.config.samplesPerDimension; i++) {
            const connectIntervalMs = Math.round(space.connectIntervalMin + i * intervalStep);

            for (let j = 0; j < this.config.samplesPerDimension; j++) {
                const dialTimeoutMs = Math.round(space.dialTimeoutMin + j * dialTimeoutStep);

                // Ensure we stay within bounds and respect step size
                const clampedInterval = Math.max(this.config.initialSpace.connectIntervalMin,
                    Math.min(this.config.initialSpace.connectIntervalMax,
                        Math.round(connectIntervalMs / this.config.stepSize) * this.config.stepSize));

                const clampedDialTimeout = Math.max(this.config.initialSpace.dialTimeoutMin,
                    Math.min(this.config.initialSpace.dialTimeoutMax,
                        Math.round(dialTimeoutMs / this.config.stepSize) * this.config.stepSize));

                candidates.push({
                    connectIntervalMs: clampedInterval,
                    dialTimeoutMs: clampedDialTimeout
                });
            }
        }

        // Remove duplicates
        const uniqueCandidates = candidates.filter((candidate, index, self) =>
            index === self.findIndex(c => c.connectIntervalMs === candidate.connectIntervalMs && c.dialTimeoutMs === candidate.dialTimeoutMs)
        );

        return uniqueCandidates;
    }

    private async testCandidates(
        candidates: Array<{ connectIntervalMs: number, dialTimeoutMs: number }>,
        iteration: number
    ): Promise<OptimizationCandidate[]> {
        const results: OptimizationCandidate[] = [];

        for (let i = 0; i < candidates.length; i++) {
            const candidate = candidates[i];
            console.log(`\n🧪 [${iteration}, ${i + 1}/${candidates.length}] Testing: interval=${candidate.connectIntervalMs}ms, dialTimeout=${candidate.dialTimeoutMs}ms`);

            const successfulTimes: number[] = [];
            let failedAttempts = 0;

            for (let attempt = 1; attempt <= this.config.attemptsPerCandidate; attempt++) {
                console.log(`  Attempt ${attempt}/${this.config.attemptsPerCandidate}...`);

                try {
                    const h = await this.createLibp2pNode();

                    try {
                        const result = await discoverOptimalConnectPath(
                            h,
                            this.targetPeerId,
                            {
                                dialTimeout: candidate.dialTimeoutMs,
                                standardInterval: candidate.connectIntervalMs
                            }
                        );

                        if (result.addr) {
                            successfulTimes.push(result.totalTime);
                            console.log(`    ✅ Success in ${result.totalTime.toFixed(2)}s (${result.method})`);
                        } else {
                            failedAttempts++;
                            console.log(`    ❌ Failed: ${result}`);
                        }
                    } finally {
                        await h.stop();
                    }

                    if (attempt < this.config.attemptsPerCandidate) {
                        await new Promise(resolve => setTimeout(resolve, this.config.cooldownMs));
                    }
                } catch (error) {
                    failedAttempts++;
                    console.log(`    💥 Error: ${error instanceof Error ? error.message : String(error)}`);
                }
            }

            const successRate = successfulTimes.length / this.config.attemptsPerCandidate;
            const geometricMean = this.calculateGeometricMean(successfulTimes);

            // Scoring function: prioritize success rate, then speed
            // Score = success_rate * 100 - geometric_mean (higher is better)
            const score = successRate > 0 ? successRate * 100 - geometricMean : -1000;

            const optimizationCandidate: OptimizationCandidate = {
                connectIntervalMs: candidate.connectIntervalMs,
                dialTimeoutMs: candidate.dialTimeoutMs,
                score,
                successRate,
                geometricMean,
                attempts: this.config.attemptsPerCandidate,
                successfulTimes,
                failedAttempts
            };

            results.push(optimizationCandidate);

            console.log(`  📊 Result: success=${(successRate * 100).toFixed(1)}%, mean=${geometricMean.toFixed(2)}s, score=${score.toFixed(2)}`);
        }

        return results;
    }

    private findBestCandidate(candidates: OptimizationCandidate[]): OptimizationCandidate | null {
        const successfulCandidates = candidates.filter(c => c.successRate > 0);
        if (successfulCandidates.length === 0) return null;

        return successfulCandidates.reduce((best, current) =>
            current.score > best.score ? current : best
        );
    }

    private refineSearchSpace(currentSpace: SearchSpace, bestCandidate: OptimizationCandidate): SearchSpace {
        // Calculate refinement radius (smaller as we converge)
        const intervalRange = currentSpace.connectIntervalMax - currentSpace.connectIntervalMin;
        const dialTimeoutRange = currentSpace.dialTimeoutMax - currentSpace.dialTimeoutMin;

        // Use 40% of current range for next iteration (zoom in)
        const intervalRadius = Math.max(this.config.stepSize * 2, intervalRange * 0.4);
        const dialTimeoutRadius = Math.max(this.config.stepSize * 2, dialTimeoutRange * 0.4);

        const newSpace: SearchSpace = {
            connectIntervalMin: Math.max(this.config.initialSpace.connectIntervalMin, bestCandidate.connectIntervalMs - intervalRadius),
            connectIntervalMax: Math.min(this.config.initialSpace.connectIntervalMax, bestCandidate.connectIntervalMs + intervalRadius),
            dialTimeoutMin: Math.max(this.config.initialSpace.dialTimeoutMin, bestCandidate.dialTimeoutMs - dialTimeoutRadius),
            dialTimeoutMax: Math.min(this.config.initialSpace.dialTimeoutMax, bestCandidate.dialTimeoutMs + dialTimeoutRadius)
        };

        return newSpace;
    }

    private async getTargetPeerIdFromRelay(): Promise<string> {
        try {
            // Ensure util server is running
            const { getUtilServer, isUtilServerAccessible } = await import('../../src/util/util-server');
            const utilServer = getUtilServer();
            if (!(await isUtilServerAccessible())) {
                console.log('Starting util server...');
                await utilServer.startServer();
            }

            const relayInfo = await utilServer.getRelayNodeInfo();
            const libp2pMa = relayInfo.libp2p_ma;
            const parts = libp2pMa.split('/');
            const targetPeerId = parts[parts.length - 1];
            console.log(`🎯 Extracted target peer ID: ${targetPeerId}`);
            return targetPeerId;
        } catch (error) {
            throw new Error(`Failed to fetch relay node: ${error instanceof Error ? error.message : String(error)}`);
        }
    }

    private createLibp2pNode(): Promise<Libp2p> {
        return createLibp2pHost();
    }

    private printFinalResults(results: OptimizationCandidate[]): void {
        console.log('\n' + '='.repeat(100));
        console.log('🏆 ADAPTIVE OPTIMIZATION RESULTS');
        console.log('='.repeat(100));

        if (results.length === 0) {
            console.log('❌ No successful combinations found. Check your network and relay setup.');
            return;
        }

        console.log(`${'Rank'.padEnd(4)} | ${'Interval'.padEnd(10)} | ${'DialTimeout'.padEnd(12)} | ${'Success Rate'.padEnd(12)} | ${'Geo Mean'.padEnd(10)} | ${'Score'.padEnd(8)} | ${'Times'.padEnd(20)}`);
        console.log('-'.repeat(100));

        results.forEach((result, index) => {
            const timesStr = result.successfulTimes.map(t => `${t.toFixed(1)}s`).join(', ');
            const mark = index === 0 ? '🥇' : index === 1 ? '🥈' : index === 2 ? '🥉' : `${index + 1}.`.padEnd(3);

            console.log(
                `${mark} | ` +
                `${result.connectIntervalMs.toString().padEnd(6)}ms | ` +
                `${result.dialTimeoutMs.toString().padEnd(8)}ms | ` +
                `${(result.successRate * 100).toFixed(1).padEnd(9)}% | ` +
                `${result.geometricMean.toFixed(2).padEnd(8)}s | ` +
                `${result.score.toFixed(2).padEnd(8)} | ` +
                `${timesStr.substring(0, 20).padEnd(20)}`
            );
        });

        const optimal = results[0];
        console.log('\n🎯 RECOMMENDED OPTIMAL COMBINATION:');
        console.log(`   Interval: ${optimal.connectIntervalMs}ms`);
        console.log(`   Dial Timeout: ${optimal.dialTimeoutMs}ms`);
        console.log(`   Success Rate: ${(optimal.successRate * 100).toFixed(1)}%`);
        console.log(`   Geometric Mean: ${optimal.geometricMean.toFixed(2)}s`);
        console.log(`   Optimization Score: ${optimal.score.toFixed(2)}`);

        console.log('\n📝 To use this combination:');
        console.log(`   // Update discoverOptimalConnectPath calls to use these options:`);
        console.log(`   // { connectIntervalMs: ${optimal.connectIntervalMs}, dialTimeoutMs: ${optimal.dialTimeoutMs} }`);

        console.log(`\n🧮 Algorithm explored ${this.allResults.length} total combinations using adaptive refinement`);
        console.log(`📊 Efficiency: ~${((1 - this.allResults.length / 3600) * 100).toFixed(1)}% reduction vs brute force (${this.allResults.length}/3600 combinations tested)`);
    }

    /**
     * Adaptive Grid Search with Progressive Refinement
     * 
     * This algorithm implements a divide-and-conquer approach:
     * 1. Start with a coarse grid over the entire search space
     * 2. Test candidates and find the best performing region
     * 3. Zoom into the best region and refine the search
     * 4. Repeat until convergence or max iterations
     * 
     * Similar to:
     * - Coordinate Descent Optimization
     * - Bayesian Optimization with Grid Search
     * - Successive Halving Algorithm
     */
    async optimize(): Promise<OptimizationCandidate[]> {
        this.targetPeerId = await this.getTargetPeerIdFromRelay();

        let currentSpace = { ...this.config.initialSpace };
        let iteration = 0;

        console.log(`🚀 Starting Adaptive Optimization for peer: ${this.targetPeerId}`);
        console.log(`📊 Algorithm: Adaptive Grid Search with Progressive Refinement`);
        console.log(`🎯 Max iterations: ${this.config.maxIterations}`);
        console.log(`📐 Samples per dimension: ${this.config.samplesPerDimension}`);
        console.log(`🔄 Attempts per candidate: ${this.config.attemptsPerCandidate}`);

        while (iteration < this.config.maxIterations) {
            iteration++;

            console.log(`\n🔍 === ITERATION ${iteration}/${this.config.maxIterations} ===`);
            console.log(`📏 Search space: interval[${currentSpace.connectIntervalMin}-${currentSpace.connectIntervalMax}], dialTimeout[${currentSpace.dialTimeoutMin}-${currentSpace.dialTimeoutMax}]`);

            // Generate candidates for current search space
            const candidates = this.generateCandidates(currentSpace);
            console.log(`🧪 Testing ${candidates.length} candidates in this iteration`);

            // Test all candidates
            const iterationResults = await this.testCandidates(candidates, iteration);
            this.allResults.push(...iterationResults);

            // Find best candidate from this iteration
            const bestCandidate = this.findBestCandidate(iterationResults);

            if (!bestCandidate) {
                console.log(`❌ No successful candidates in iteration ${iteration}, stopping optimization`);
                break;
            }

            console.log(`🏆 Best candidate: interval=${bestCandidate.connectIntervalMs}ms, dialTimeout=${bestCandidate.dialTimeoutMs}ms, score=${bestCandidate.score.toFixed(2)}`);

            // Check for convergence
            const spaceSize = this.calculateSpaceSize(currentSpace);
            const originalSpaceSize = this.calculateSpaceSize(this.config.initialSpace);
            const convergenceRatio = spaceSize / originalSpaceSize;

            console.log(`📐 Search space size ratio: ${(convergenceRatio * 100).toFixed(1)}%`);

            if (convergenceRatio < this.config.convergenceThreshold) {
                console.log(`✅ Converged! Search space reduced to ${(convergenceRatio * 100).toFixed(1)}% of original`);
                break;
            }

            // Refine search space around best candidate
            currentSpace = this.refineSearchSpace(currentSpace, bestCandidate);
        }

        // Return top results across all iterations
        const topResults = this.allResults
            .filter(r => r.successRate > 0)
            .sort((a, b) => b.score - a.score)
            .slice(0, 10);

        this.printFinalResults(topResults);
        return topResults;
    }
}

// Vitest test suites for different environments
describe('Peer Discovery Optimization', () => {
    test('Multi-Environment - Find optimal standardInterval and dialTimeout', async () => {
        const environment = typeof window !== 'undefined' ? 'browser' : 'node';
        const browserName = typeof window !== 'undefined' ?
            (navigator.userAgent.includes('Chrome') ? 'Chrome' :
                navigator.userAgent.includes('Firefox') ? 'Firefox' : 'Browser') : 'Node.js';

        console.log(`\n🚀 Testing in ${browserName} Environment`);
        console.log('='.repeat(40));

        // Check if libp2p functions are available in browser context
        if (environment === 'browser') {
            try {
                // Test if we can access the required functions
                if (typeof createLibp2pHost === 'undefined' || typeof discoverOptimalConnectPath === 'undefined') {
                    console.log('⚠️ Browser environment detected but libp2p functions not available');
                    console.log('🔄 Skipping optimization test for browser environment');
                    expect(true).toBe(true); // Pass the test but skip actual optimization
                    return;
                }
            } catch (error: any) {
                console.log(`⚠️ Browser compatibility issue: ${error?.message || 'Unknown error'}`);
                console.log('🔄 Skipping optimization test for browser environment');
                expect(true).toBe(true); // Pass the test but skip actual optimization
                return;
            }
        }

        // Choose config based on environment
        const isBrowser = typeof window !== 'undefined';
        // const config = isBrowser ? BROWSER_CONFIG : FAST_CONFIG;
        const config = DEFAULT_CONFIG

        const optimizer = new AdaptiveOptimizer(config);
        console.log(`📋 Configuration for ${browserName}:`, config);

        const results = await optimizer.optimize();

        console.log(`\n📊 ${browserName.toUpperCase()} OPTIMIZATION RESULTS:`);
        console.log('='.repeat(50));

        if (results.length === 0) {
            console.log('❌ No successful optimization results found');
            expect(false).toBe(true); // Fail the test if no results
            return;
        }

        // Display top 3 results
        const topResults = results.slice(0, 3);
        topResults.forEach((result, index) => {
            console.log(`\n🏆 Rank ${index + 1}:`);
            console.log(`   standardInterval: ${result.connectIntervalMs}ms`);
            console.log(`   dialTimeout: ${result.dialTimeoutMs}ms`);
            console.log(`   Score: ${result.score.toFixed(2)}`);
            console.log(`   Success Rate: ${(result.successRate * 100).toFixed(1)}%`);
            console.log(`   Average Time: ${result.geometricMean.toFixed(2)}s`);
            console.log(`   Attempts: ${result.attempts}`);
        });

        // Environment-specific optimal settings summary
        const optimal = results[0];
        console.log(`\n🎯 OPTIMAL ${browserName.toUpperCase()} SETTINGS:`);
        console.log(`   standardInterval: ${optimal.connectIntervalMs}ms`);
        console.log(`   dialTimeout: ${optimal.dialTimeoutMs}ms`);
        console.log(`   Expected Success Rate: ${(optimal.successRate * 100).toFixed(1)}%`);
        console.log(`   Expected Average Discovery Time: ${optimal.geometricMean.toFixed(2)}s`);

        // Store results for comparison (if running multiple environments)
        try {
            const global = globalThis as any;
            if (!global.optimizationResults) {
                global.optimizationResults = {};
            }
            global.optimizationResults[browserName] = {
                optimal: optimal,
                topResults: topResults
            };
        } catch (e) {
            // Ignore global storage errors
        }

        // Assertions
        expect(results.length).toBeGreaterThan(0);
        expect(results[0].successRate).toBeGreaterThan(0);

        console.log(`\n✅ ${browserName} optimization completed with ${results.length} successful results`);
        console.log(`🏆 Best ${browserName} combination: ${results[0].connectIntervalMs}ms interval, ${results[0].dialTimeoutMs}ms dialTimeout`);

        // Output comparison data if multiple environments tested
        try {
            const global = globalThis as any;
            if (typeof global.optimizationResults === 'object' &&
                Object.keys(global.optimizationResults).length > 1) {
                console.log('\n🔄 CROSS-ENVIRONMENT COMPARISON:');
                console.log('='.repeat(50));
                Object.entries(global.optimizationResults).forEach(([env, data]: [string, any]) => {
                    console.log(`${env}: ${data.optimal.connectIntervalMs}ms/${data.optimal.dialTimeoutMs}ms (${(data.optimal.successRate * 100).toFixed(1)}% success)`);
                });
            }
        } catch (e) {
            // Ignore comparison errors
        }
    }, 24 * 36e5); // 24 hours timeout
});

// Export for use in other files
export { AdaptiveOptimizer, type OptimizationCandidate, type OptimizationConfig, FAST_CONFIG, BROWSER_CONFIG };
