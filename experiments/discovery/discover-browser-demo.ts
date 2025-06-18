import { getTestCases, runSingleTest, runAllTests } from './discover-path';
import { createLibp2pHost } from "../../src/client/core/libp2p-host";

// Real-time metrics tracking
let testsRun = 0;
let testsPassed = 0;
let testTimes: number[] = [];
let isRunning = false;

function log(message: string) {
    const logs = document.getElementById('logs');
    if (logs) {
        const timestamp = new Date().toLocaleTimeString();
        logs.innerHTML += `<div>[${timestamp}] ${message}</div>`;
        logs.scrollTop = logs.scrollHeight;
        console.log(`[${timestamp}] ${message}`);
    }
}

function updateStatus(message: string, type = 'info') {
    const status = document.getElementById('status');
    if (status) {
        status.textContent = message;
        status.className = `status ${type}`;
    }
}

function updateMetrics() {
    // Update tests run
    const testsRunEl = document.getElementById('tests-run');
    if (testsRunEl) testsRunEl.textContent = testsRun.toString();

    // Update tests passed
    const testsPassedEl = document.getElementById('tests-passed');
    if (testsPassedEl) testsPassedEl.textContent = testsPassed.toString();

    // Update average time
    const avgTimeEl = document.getElementById('avg-time');
    if (avgTimeEl && testTimes.length > 0) {
        const avgTime = testTimes.reduce((a, b) => a + b, 0) / testTimes.length;
        avgTimeEl.textContent = `${Math.round(avgTime)}ms`;
    } else if (avgTimeEl) {
        avgTimeEl.textContent = '0ms';
    }

    // Update success rate
    const successRateEl = document.getElementById('success-rate');
    if (successRateEl) {
        const rate = testsRun > 0 ? Math.round((testsPassed / testsRun) * 100) : 0;
        successRateEl.textContent = `${rate}%`;
    }
}

function resetMetrics() {
    testsRun = 0;
    testsPassed = 0;
    testTimes = [];
    updateMetrics();
}

function clearLogs() {
    const logs = document.getElementById('logs');
    if (logs) {
        logs.innerHTML = '<div>🌐 Logs cleared - Ready for next test run</div>';
    }
}

function setButtonState(enabled: boolean) {
    const runBtn = document.getElementById('run-btn') as HTMLButtonElement;
    if (runBtn) {
        runBtn.disabled = !enabled;
        runBtn.textContent = enabled ? '🧪 Run Discovery Tests' : '⏳ Running Tests...';
    }
}

async function runDemo() {
    if (isRunning) {
        log('⚠️ Tests are already running!');
        return;
    }

    try {
        isRunning = true;
        setButtonState(false);
        resetMetrics();
        
        log('🚀 Starting LibP2P Auto-Relay Discovery Tests...');
        updateStatus('🔄 Initializing tests...', 'info');
        
        // Initialize libp2p host
        const h = await createLibp2pHost();
        const testCases = await getTestCases();
        
        log(`📋 Loaded ${testCases.length} test scenarios`);
        updateStatus('🧪 Running test scenarios...', 'info');
        
        // Run each test individually with UI updates
        for (let i = 0; i < testCases.length; i++) {
            const testCase = testCases[i];
            
            log(`\n🧪 Running Test ${i + 1}/4: ${testCase.name}`);
            log(`📝 ${testCase.description}`);
            log(`🔗 Input: ${testCase.input}`);
            updateStatus(`Running Test ${i + 1}/4: ${testCase.name}`, 'info');
            
            const startTime = Date.now();
            const testResult = await runSingleTest(h, testCase, i);
            const actualTestTime = Date.now() - startTime;
            
            // Update metrics immediately
            testsRun++;
            if (testResult.success) {
                testsPassed++;
                log(`✅ Test ${i + 1} PASSED (${actualTestTime}ms)`);
                if (testResult.result?.addr) {
                    log(`   └─ Address: ${testResult.result.addr}`);
                    log(`   └─ Method: ${testResult.result.method} (${testResult.result.trackDescription})`);
                    log(`   └─ Status: ${testResult.result.status}`);
                    log(`   └─ Connect time: ${testResult.result.connectTime}ms`);
                }
                if (testResult.verificationSuccess !== undefined) {
                    log(`🔍 Connection verification: ${testResult.verificationSuccess ? '✅' : '❌'} (${testResult.verificationTime}ms)`);
                }
            } else {
                log(`❌ Test ${i + 1} FAILED: ${testResult.error || 'Unknown error'}`);
            }
            
            testTimes.push(actualTestTime);
            updateMetrics();
            
            // Short delay between tests (except last one)
            if (i < testCases.length - 1) {
                log('⏳ Waiting 2s before next test...');
                updateStatus(`Completed ${i + 1}/4 tests - waiting 2s...`, 'info');
                await new Promise(resolve => setTimeout(resolve, 2000));
            }
        }
        
        await h.stop();
        
        const successRate = Math.round((testsPassed / testsRun) * 100);
        log(`\n🏁 All tests completed! ${testsPassed}/${testsRun} passed (${successRate}%)`);
        updateStatus(`✅ All tests completed! ${testsPassed}/${testsRun} passed`, 'success');
        
    } catch (error: any) {
        log(`❌ Demo failed: ${error.message}`);
        updateStatus('❌ Tests failed', 'error');
        console.error('Demo error:', error);
    } finally {
        isRunning = false;
        setButtonState(true);
    }
}

function restartDemo() {
    if (isRunning) {
        log('⚠️ Cannot restart while tests are running');
        return;
    }
    
    clearLogs();
    resetMetrics();
    updateStatus('🔄 Ready to run tests', 'info');
    log('🔄 Demo restarted - Click "Run Discovery Tests" to begin');
}

// Make functions globally available
(window as any).runDemo = runDemo;
(window as any).clearLogs = clearLogs;
(window as any).restartDemo = restartDemo;

// Initialize when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    updateStatus('✅ Ready to run tests - ESM modules loaded', 'info');
    log('✅ Browser demo initialized successfully');
    log('📋 4 test scenarios loaded:');
    log('   • Type 1: Circuit relay path');
    log('   • Type 2: P2P multiaddr only');
    log('   • Type 3: Direct multiaddr');
    log('   • Type 4: Raw peer ID');
    log('🚀 Click "Run Discovery Tests" to start!');
    
    setButtonState(true);
    resetMetrics();
});

// Export runAllTests function as main for backwards compatibility
export { runAllTests as main };
