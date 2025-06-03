import { main as debugMain } from './debug';

console.log('✅ Browser demo ESM imports successful');

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

function clearLogs() {
    const logs = document.getElementById('logs');
    if (logs) {
        logs.innerHTML = '<div>🌐 Logs cleared</div>';
    }
}

async function runDemo() {
    log('🧪 Starting discovery tests...');
    updateStatus('Running discovery tests...', 'info');

    const runBtn = document.getElementById('run-btn') as HTMLButtonElement;
    if (runBtn) {
        runBtn.disabled = true;
        runBtn.textContent = '🔄 Running...';
    }

    try {
        log('🚀 Starting debug.ts main function...');
        await debugMain();

        updateStatus('✅ All tests completed!', 'success');
        log('✅ Discovery tests completed successfully!');

    } catch (error: any) {
        log(`❌ Test failed: ${error.message}`);
        log(`🔍 Error stack: ${error.stack}`);
        updateStatus('❌ Test failed', 'error');
        console.error(error);
    } finally {
        if (runBtn) {
            runBtn.disabled = false;
            runBtn.textContent = '🧪 Run Discovery Tests';
        }
    }
}

function restartDemo() {
    log('🔄 Restarting demo...');
    updateStatus('Ready to test', 'info');

    const runBtn = document.getElementById('run-btn') as HTMLButtonElement;
    if (runBtn) {
        runBtn.disabled = false;
        runBtn.textContent = '🧪 Run Discovery Tests';
    }

    clearLogs();
    log('🌐 Browser Demo Restarted');
}

// Make functions globally available for onclick handlers
(window as any).runDemo = runDemo;
(window as any).clearLogs = clearLogs;
(window as any).restartDemo = restartDemo;
(window as any).log = log;
(window as any).updateStatus = updateStatus;

// Initialize when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    updateStatus('Ready to test', 'info');
    log('✅ Browser Demo loaded successfully');
    log('📋 Click "Run Discovery Tests" to start the libp2p auto-relay discovery tests');
});
