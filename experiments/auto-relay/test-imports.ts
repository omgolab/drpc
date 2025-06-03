import { discoverOptimalConnectPath } from './discover';
import { main as debugMain } from './debug';

// Make functions globally available
(window as any).discoverOptimalConnectPath = discoverOptimalConnectPath;
(window as any).debugMain = debugMain;

console.log('✅ ESM imports successful');

function log(message: string) {
    const logs = document.getElementById('logs');
    if (logs) {
        const timestamp = new Date().toLocaleTimeString();
        logs.innerHTML += `<div>[${timestamp}] ${message}</div>`;
        logs.scrollTop = logs.scrollHeight;
        // also log to console for debugging
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

function clearLogs() {
    const logs = document.getElementById('logs');
    if (logs) {
        logs.innerHTML = '<div>🌐 Logs cleared</div>';
    }
}

async function testImports() {
    log('🧪 Testing imports...');
    updateStatus('Testing imports...', 'info');

    try {        // Test that functions are available
        log('✅ Testing imported functions...');
        log(`📦 discoverOptimalConnectPath: ${typeof discoverOptimalConnectPath}`);
        log('✅ discoverOptimalConnectPath function found!');

        // Test debug function
        log(`📦 debugMain: ${typeof debugMain}`);
        
        updateStatus('✅ Running debug tests...', 'info');
        log('🚀 Starting debug.ts main function...');
        
        await debugMain();

        updateStatus('✅ All tests completed!', 'success');

    } catch (error: any) {
        log(`❌ Test failed: ${error.message}`);
        log(`🔍 Error stack: ${error.stack}`);
        updateStatus('❌ Test failed', 'error');
        console.error(error);
    }
}

// Make functions globally available immediately
(window as any).testImports = testImports;
(window as any).clearLogs = clearLogs;
(window as any).log = log;
(window as any).updateStatus = updateStatus;

// Update status to show module is loaded
document.addEventListener('DOMContentLoaded', () => {
    updateStatus('Ready to test - ESM modules loaded', 'info');
    log('✅ ESM modules loaded successfully');
});
