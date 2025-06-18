import { describe, it, expect } from 'vitest';
import {
    runAllTests,
    testCircuitRelayPath,
    testP2PMultiaddr,
    testDirectMultiaddr,
    testRawPeerId,
} from './discover-path';

describe('Discovery cold-path(individual libp2p node) tests', () => {
    describe('Individual Address Type Tests', () => {
        it('should test circuit relay path (Type 1)', async () => {
            const result = await testCircuitRelayPath();

            expect(result).toBeDefined();
            expect(result).toHaveProperty('success');
            expect(result).toHaveProperty('testTime');
            expect(typeof result.success).toBe('boolean');
            expect(typeof result.testTime).toBe('number');

            // Only count as test success if result.success is true
            if (result.success) {
                expect(result).toHaveProperty('result');
                expect(result).toHaveProperty('verificationSuccess');
                expect(result).toHaveProperty('verificationTime');
                console.log('✅ Circuit relay test succeeded');
            } else {
                expect(result).toHaveProperty('error');
                // Fail with descriptive message instead of generic true/false
                throw new Error(`Circuit relay path discovery failed: ${result.error}`);
            }
        });

        it.only('should test p2p multiaddr (Type 2)', async () => {
            const result = await testP2PMultiaddr();

            expect(result).toBeDefined();
            expect(result).toHaveProperty('success');
            expect(result).toHaveProperty('testTime');
            expect(typeof result.success).toBe('boolean');
            expect(typeof result.testTime).toBe('number');

            // Only count as test success if result.success is true
            if (result.success) {
                expect(result).toHaveProperty('result');
                expect(result).toHaveProperty('verificationSuccess');
                expect(result).toHaveProperty('verificationTime');
                console.log('✅ P2P multiaddr test succeeded');
            } else {
                expect(result).toHaveProperty('error');
                // Fail with descriptive message instead of generic true/false
                throw new Error(`P2P multiaddr discovery failed: ${result.error}`);
            }
        });

        it('should test direct multiaddr (may contain wrong ip/port but correct peer ID) (Type 3)', async () => {
            const result = await testDirectMultiaddr();

            expect(result).toBeDefined();
            expect(result).toHaveProperty('success');
            expect(result).toHaveProperty('testTime');
            expect(typeof result.success).toBe('boolean');
            expect(typeof result.testTime).toBe('number');

            // Only count as test success if result.success is true
            if (result.success) {
                expect(result).toHaveProperty('result');
                expect(result).toHaveProperty('verificationSuccess');
                expect(result).toHaveProperty('verificationTime');
                console.log('✅ Direct multiaddr test succeeded');
            } else {
                expect(result).toHaveProperty('error');
                // Fail with descriptive message instead of generic true/false
                throw new Error(`Direct multiaddr discovery failed: ${result.error}`);
            }
        });

        it('should test raw peer ID (Type 4)', async () => {
            const result = await testRawPeerId();

            expect(result).toBeDefined();
            expect(result).toHaveProperty('success');
            expect(result).toHaveProperty('testTime');
            expect(typeof result.success).toBe('boolean');
            expect(typeof result.testTime).toBe('number');

            // Only count as test success if result.success is true
            if (result.success) {
                expect(result).toHaveProperty('result');
                expect(result).toHaveProperty('verificationSuccess');
                expect(result).toHaveProperty('verificationTime');
                console.log('✅ Raw peer ID test succeeded');
            } else {
                expect(result).toHaveProperty('error');
                // Fail with descriptive message instead of generic true/false
                throw new Error(`Raw peer ID discovery failed: ${result.error}`);
            }
        });
    });

    describe('Discover hot-path(same libp2p node) test', () => {
        it('should run all discovery path test cases with success validation', async () => {
            // This test runs individual test cases and validates each one succeeds
            const results = await Promise.all([
                testCircuitRelayPath(),
                testP2PMultiaddr(),
                testDirectMultiaddr(),
                testRawPeerId()
            ]);

            // Ensure all tests completed (not just that they didn't throw)
            expect(results).toHaveLength(4);

            // Check that each result has the expected structure
            results.forEach((result, index) => {
                expect(result).toBeDefined();
                expect(result).toHaveProperty('success');
                expect(typeof result.success).toBe('boolean');
            });

            // Collect all failed tests with descriptive messages
            const failedTests = results.map((result, index) => ({
                testNumber: index + 1,
                testName: ['Circuit Relay', 'P2P Multiaddr', 'Direct Multiaddr', 'Raw Peer ID'][index],
                error: result.error
            })).filter((_, index) => !results[index].success);

            if (failedTests.length > 0) {
                const failureMessages = failedTests.map(test =>
                    `Test ${test.testNumber} (${test.testName}): ${test.error}`
                ).join('\n  ');
                throw new Error(`${failedTests.length}/${results.length} discovery tests failed:\n  ${failureMessages}`);
            }
        }); // Uses vitest config timeout (15 minutes)
    });
});
