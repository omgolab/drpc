/**
 * Utility Server Manager
 * 
 * This module provides a singleton UtilServer instance that can be used across
 * all integration tests and where needed. The server automatically starts/stops as needed and
 * includes a default timeout mechanism.
 */

// Browser-safe imports and types
let spawn: any, tmpdir: any, join: any, fs: any;

interface BrowserChildProcess {
    pid?: number;
    kill(signal?: string): void;
    on(event: string, callback: (...args: any[]) => void): void;
    stdout?: { on(event: string, callback: (data: any) => void): void };
    stderr?: { on(event: string, callback: (data: any) => void): void };
    killed?: boolean;
}


// Only use Node.js modules if running in Node.js environment
const isNodeEnvironment = typeof process !== 'undefined' && process.versions?.node;
let nodeModulesInitialized = false;

async function initializeNodeModules() {
    if (nodeModulesInitialized || !isNodeEnvironment) return;

    try {
        // Dynamic imports for Node.js modules to avoid browser bundler issues
        const { spawn: nodeSpawn } = await import('child_process');
        const { tmpdir: nodeTmpdir } = await import('os');
        const { join: nodeJoin } = await import('path');
        const { promises: nodeFs } = await import('fs');

        spawn = nodeSpawn;
        tmpdir = nodeTmpdir;
        join = nodeJoin;
        fs = nodeFs;
        nodeModulesInitialized = true;
    } catch (error) {
        console.warn('Failed to initialize Node.js modules:', error);
    }
}

if (isNodeEnvironment) {
    // Initialize Node.js modules
    initializeNodeModules();
} else {
    // Browser stubs
    spawn = () => ({ kill: () => { }, on: () => { } });
    tmpdir = () => '/tmp';
    join = (...args: string[]) => args.join('/');
    fs = {
        mkdir: () => Promise.resolve(),
        unlink: () => Promise.resolve()
    };
}

export interface NodeInfo {
    http_address: string;
    libp2p_ma: string;
}

/**
 * Utility Server Helper - manages the Go utility server lifecycle
 */
export class UtilServerHelper {
    private serverProcess: BrowserChildProcess | null = null;
    private port: number;
    private binPath: string;
    private isReady: boolean = false;
    private startupPromise: Promise<void> | null = null;
    private timeoutId: NodeJS.Timeout | null = null;
    private readonly DEFAULT_TIMEOUT = 30 * 60 * 1000; // 30 minutes
    private initialized: boolean = false;

    constructor(port: number = 8080) {
        this.port = port;
        // Initialize binPath after Node modules are loaded
        this.binPath = '';
        this.init();
    }

    private async init() {
        if (this.initialized) return;

        if (isNodeEnvironment) {
            await initializeNodeModules();
            this.binPath = join(tmpdir(), 'tmp', `util-server-${this.port}`);
        } else {
            this.binPath = `/tmp/util-server-${this.port}`;
        }

        this.initialized = true;
    }

    private async ensureInitialized() {
        if (!this.initialized) {
            await this.init();
        }
    }

    /**
     * Start the utility server
     */
    async startServer(): Promise<void> {
        await this.ensureInitialized();

        // Browser environments can't start servers
        if (!isNodeEnvironment) {
            console.warn('⚠️ Util server cannot be started in browser environment');
            return;
        }

        // If already starting, wait for it
        if (this.startupPromise) {
            await this.startupPromise;
            return;
        }

        // If already running, no need to start again
        if (this.isReady) {
            console.log('🔄 Server already running...');
            // await this.stopServer();
            return;
        }

        this.startupPromise = this._doStartServer();
        try {
            await this.startupPromise;
        } finally {
            this.startupPromise = null;
        }
    }

    private async _doStartServer(): Promise<void> {
        try {
            // Build the utility server
            await this.buildServer();

            // Start the server process
            await this.startServerProcess();

            // Wait for server to be ready
            await this.waitForServerReady();

            // Set up auto-timeout
            this.setupAutoTimeout();

            this.isReady = true;
            console.log('✅ Utility server is ready and accessible');

        } catch (error) {
            console.error('❌ Failed to start utility server:', error);
            await this.cleanup();
            throw error;
        }
    }

    /**
     * Build the utility server binary
     */
    private async buildServer(): Promise<void> {
        console.log(`Building utility server: go build -o ${this.binPath} cmd/util-server/main.go`);

        // Create tmp directory if it doesn't exist
        await fs.mkdir(join(tmpdir(), 'tmp'), { recursive: true });

        return new Promise((resolve, reject) => {
            const buildProcess = spawn('go', ['build', '-o', this.binPath, 'cmd/util-server/main.go'], {
                stdio: 'pipe'
            });

            let stderr = '';
            buildProcess.stderr?.on('data', (data: Buffer) => {
                stderr += data.toString();
            });

            buildProcess.on('close', (code: number | null) => {
                if (code === 0) {
                    console.log(`Successfully built utility server binary: ${this.binPath}`);
                    resolve();
                } else {
                    reject(new Error(`Build failed with code ${code}: ${stderr}`));
                }
            });

            buildProcess.on('error', (error: Error) => {
                reject(new Error(`Build process error: ${error.message}`));
            });
        });
    }

    /**
     * Start the server process
     */
    private async startServerProcess(): Promise<void> {
        console.log(`Starting utility server binary: ${this.binPath}`);

        return new Promise((resolve, reject) => {
            this.serverProcess = spawn(this.binPath, [], {
                stdio: ['ignore', 'pipe', 'pipe'],
                env: { ...process.env, PORT: this.port.toString() }
            });

            if (!this.serverProcess) {
                reject(new Error('Failed to start server process'));
                return;
            }

            // Handle server stdout
            this.serverProcess.stdout?.on('data', (data) => {
                const output = data.toString().trim();
                if (output) {
                    console.log(`[UtilServer STDOUT]: ${output}`);
                }
            });

            // Handle server stderr
            this.serverProcess.stderr?.on('data', (data) => {
                const output = data.toString().trim();
                if (output) {
                    console.error(`[UtilServer STDERR]: ${output}`);
                }
            });

            // Handle process exit
            this.serverProcess.on('close', (code, signal) => {
                console.log(`Utility server process exited with code ${code}`);
                this.isReady = false;
                this.serverProcess = null;
            });

            this.serverProcess.on('error', (error) => {
                console.error('Server process error:', error);
                reject(error);
            });

            // Give the server a moment to start
            setTimeout(resolve, 1000);
        });
    }

    /**
     * Wait for server to be ready by polling the health endpoint
     */
    private async waitForServerReady(maxAttempts: number = 30): Promise<void> {
        const baseUrl = `http://localhost:${this.port}`;

        for (let attempt = 1; attempt <= maxAttempts; attempt++) {
            try {
                const response = await fetch(`${baseUrl}/public-node`);
                if (response.ok) {
                    console.log('Utility server is ready.');
                    return;
                }
            } catch (error) {
                // Server not ready yet, continue waiting
            }

            if (attempt < maxAttempts) {
                await new Promise(resolve => setTimeout(resolve, 1000));
            }
        }

        throw new Error(`Server did not become ready after ${maxAttempts} attempts`);
    }

    /**
     * Set up auto-timeout to stop server after inactivity
     */
    private setupAutoTimeout(): void {
        if (this.timeoutId) {
            clearTimeout(this.timeoutId);
        }

        this.timeoutId = setTimeout(async () => {
            console.log('⏰ Utility server auto-timeout reached, stopping server...');
            await this.stopServer();
        }, this.DEFAULT_TIMEOUT);
    }

    /**
     * Reset the auto-timeout (call when server is being used)
     */
    public resetTimeout(): void {
        if (this.isReady) {
            this.setupAutoTimeout();
        }
    }

    /**
     * Stop the utility server
     */
    async stopServer(): Promise<void> {
        if (this.timeoutId) {
            clearTimeout(this.timeoutId);
            this.timeoutId = null;
        }

        await this.cleanup();
    }

    /**
     * Cleanup server resources
     */
    private async cleanup(): Promise<void> {
        if (this.serverProcess) {
            console.log('Stopping utility server...');

            const pid = this.serverProcess.pid;
            if (pid) {
                console.log(`Stopping server process PID: ${pid}`);

                // Send SIGTERM for graceful shutdown
                this.serverProcess.kill('SIGTERM');
                console.log(`Sent SIGTERM to server process ${pid}, waiting for graceful shutdown...`);

                // Wait for process to exit
                await new Promise<void>((resolve) => {
                    if (!this.serverProcess) {
                        resolve();
                        return;
                    }

                    this.serverProcess.on('close', (code) => {
                        console.log(`Server process ${pid} exited with code ${code}`);
                        resolve();
                    });

                    // Force kill after 5 seconds if graceful shutdown fails
                    setTimeout(() => {
                        if (this.serverProcess && !this.serverProcess.killed) {
                            console.log('Forcefully killing server process...');
                            this.serverProcess.kill('SIGKILL');
                        }
                        resolve();
                    }, 5000);
                });
            }

            this.serverProcess = null;
        }

        // Clean up binary file
        try {
            await fs.unlink(this.binPath);
            console.log(`Cleaned up binary file: ${this.binPath}`);
        } catch (error) {
            // File might not exist, ignore
        }

        // Backup cleanup: kill any remaining processes
        await this.backupProcessCleanup();

        this.isReady = false;
    }

    /**
     * Backup process cleanup using system commands
     */
    private async backupProcessCleanup(): Promise<void> {
        const cleanupCmd = `pkill -f "^${this.binPath}$"`;
        console.log(`Running backup process cleanup: ${cleanupCmd}`);

        return new Promise((resolve) => {
            const cleanup = spawn('pkill', ['-f', `^${this.binPath}$`], { stdio: 'pipe' });

            cleanup.on('close', (code: number | null) => {
                if (code === 0) {
                    console.log(`Cleaned up remaining processes for ${this.binPath}`);
                } else {
                    console.log(`No remaining processes found for ${this.binPath} (expected)`);
                }
                resolve();
            });

            cleanup.on('error', () => {
                console.log('Process cleanup command not available, skipping');
                resolve();
            });
        });
    }

    /**
     * Get public node information
     */
    async getPublicNodeInfo(): Promise<NodeInfo> {
        this.resetTimeout(); // Reset timeout when server is being used

        const url = `http://localhost:${this.port}/public-node`;
        console.log(`Requesting node info from: ${url}`);

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`Failed to fetch node info: ${response.status} ${response.statusText}`);
        }

        const nodeInfo = await response.json();
        console.log(`Received node info from ${url}:`, JSON.stringify(nodeInfo));

        return nodeInfo;
    }

    /**
     * Get relay node information
     */
    async getRelayNodeInfo(): Promise<NodeInfo> {
        await this.ensureInitialized();
        this.resetTimeout(); // Reset timeout when server is being used

        const url = `http://localhost:${this.port}/relay-node`;
        console.log(`Requesting relay node info from: ${url}`);

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`Failed to fetch relay node info: ${response.status} ${response.statusText}`);
        }

        const nodeInfo = await response.json();
        console.log(`Received relay node info from ${url}:`, JSON.stringify(nodeInfo));

        return nodeInfo;
    }

    /**
     * Get gateway node information
     */
    async getGatewayNodeInfo(): Promise<NodeInfo> {
        this.resetTimeout(); // Reset timeout when server is being used

        const url = `http://localhost:${this.port}/gateway-node`;
        console.log(`Requesting gateway node info from: ${url}`);

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`Failed to fetch gateway node info: ${response.status} ${response.statusText}`);
        }

        const nodeInfo = await response.json();
        console.log(`Received gateway node info from ${url}:`, JSON.stringify(nodeInfo));

        return nodeInfo;
    }

    /**
     * Get gateway relay node information
     */
    async getGatewayRelayNodeInfo(): Promise<NodeInfo> {
        this.resetTimeout(); // Reset timeout when server is being used

        const url = `http://localhost:${this.port}/gateway-relay-node`;
        console.log(`Requesting gateway relay node info from: ${url}`);

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`Failed to fetch gateway relay node info: ${response.status} ${response.statusText}`);
        }

        const nodeInfo = await response.json();
        console.log(`Received gateway relay node info from ${url}:`, JSON.stringify(nodeInfo));

        return nodeInfo;
    }

    /**
     * Get gateway auto relay node information
     */
    async getGatewayAutoRelayNodeInfo(): Promise<NodeInfo> {
        this.resetTimeout(); // Reset timeout when server is being used

        const url = `http://localhost:${this.port}/gateway-auto-relay-node`;
        console.log(`Requesting gateway auto relay node info from: ${url}`);

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`Failed to fetch gateway auto relay node info: ${response.status} ${response.statusText}`);
        }

        const nodeInfo = await response.json();
        console.log(`Received gateway auto relay node info from ${url}:`, JSON.stringify(nodeInfo));

        return nodeInfo;
    }

    /**
     * Check if server is ready
     */
    isServerReady(): boolean {
        return this.isReady;
    }

    /**
     * Get server base URL
     */
    getBaseUrl(): string {
        return `http://localhost:${this.port}`;
    }

    /**
     * Cleanup orphaned processes
     */
    async cleanupOrphanedProcesses(): Promise<void> {
        console.log('Cleaning up any orphaned util-server processes...');

        return new Promise((resolve) => {
            const cleanup = spawn('pkill', ['-f', 'util-server'], { stdio: 'pipe' });

            cleanup.on('close', (code: number | null) => {
                if (code === 0) {
                    console.log('Cleaned up orphaned util-server processes');
                } else {
                    console.log('No orphaned util-server processes found');
                }
                console.log('Orphaned process cleanup completed.');
                resolve();
            });

            cleanup.on('error', () => {
                console.log('Process cleanup command not available, skipping orphaned cleanup');
                resolve();
            });
        });
    }

}

// Singleton instance
let _serverInstance: UtilServerHelper | null = null;

/**
 * Get the singleton UtilServer instance with all necessary methods
 * This instance provides:
 * - startServer() / stopServer()
 * - getPublicNodeInfo(), getRelayNodeInfo(), etc.
 * - cleanupOrphanedProcesses()
 * - isServerReady(), getBaseUrl()
 */
export function getUtilServer(port: number = 8080): UtilServerHelper {
    if (!_serverInstance || _serverInstance['port'] !== port) {
        _serverInstance = new UtilServerHelper(port);
    }
    return _serverInstance;
}

/**
 * Check if server is running and accessible
 */
export async function isUtilServerAccessible(port: number = 8080): Promise<boolean> {
    try {
        const response = await fetch(`http://localhost:${port}/public-node`);
        return response.ok;
    } catch {
        return false;
    }
}
