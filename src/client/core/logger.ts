/**
 * Instance-based logging utilities for dRPC
 */

/**
 * Log levels supported by the logger
 */
export enum LogLevel {
  DEBUG = "debug",
  INFO = "info",
  WARN = "warn",
  ERROR = "error",
}

/**
 * Map of log levels to their numeric priority
 */
const LOG_LEVELS: Record<LogLevel, number> = {
  [LogLevel.DEBUG]: 0,
  [LogLevel.INFO]: 1,
  [LogLevel.WARN]: 2,
  [LogLevel.ERROR]: 3,
};

/**
 * Logger configuration options
 */
export interface LoggerOptions {
  /**
   * The context name to prefix log messages with
   */
  contextName?: string;

  /**
   * The minimum log level to display
   */
  logLevel?: LogLevel;
}

/**
 * Logger interface defining the available logging methods
 */
export interface ILogger {
  debug(message: string, ...args: any[]): void;
  info(message: string, ...args: any[]): void;
  warn(message: string, ...args: any[]): void;
  error(message: string, ...args: any[]): void;
  setLogLevel(level: LogLevel): void;
  setContextName(name: string): void;

  /**
   * Check if the current log level is at least the specified level
   * For example, if current level is INFO, then:
   * - isMinLevel(DEBUG) returns false
   * - isMinLevel(INFO) returns true
   * - isMinLevel(WARN) returns true
   * - isMinLevel(ERROR) returns true
   */
  isMinLevel(level: LogLevel): boolean;

  /**
   * Create a child logger with a nested context name
   * The child logger inherits the parent's log level initially
   * but can be configured independently afterward
   */
  createChildLogger(options: LoggerOptions): ILogger;
}

/**
 * Creates a new logger instance with the specified options
 */
export function createLogger(options: LoggerOptions = {}): ILogger {
  return new Logger(options.contextName, options.logLevel);
}

/**
 * Logger implementation that supports instance-specific settings
 */
class Logger implements ILogger {
  private contextName: string;
  private logLevel: LogLevel;

  constructor(
    contextName: string = "[dRPC]",
    logLevel: LogLevel = LogLevel.ERROR,
  ) {
    this.contextName = contextName;
    this.logLevel = logLevel;
  }

  /**
   * Set the minimum log level for this logger instance
   */
  setLogLevel(level: LogLevel): void {
    this.logLevel = level;
  }

  /**
   * Set the context name (prefix) for this logger instance
   */
  setContextName(name: string): void {
    // Only add [] if not already wrapped
    if (name.startsWith("[") && name.endsWith("]")) {
      this.contextName = name;
    } else {
      this.contextName = `[${name}]`;
    }
  }

  /**
   * Check if the current log level is at least the specified level
   * For example, if current level is INFO, then:
   * - isMinLevel(DEBUG) returns false
   * - isMinLevel(INFO) returns true
   * - isMinLevel(WARN) returns true
   * - isMinLevel(ERROR) returns true
   */
  isMinLevel(level: LogLevel): boolean {
    return LOG_LEVELS[level] >= LOG_LEVELS[this.logLevel];
  }

  /**
   * Create a child logger with a nested context name
   * The child logger inherits the parent's log level initially
   * but can be configured independently afterward
   */
  createChildLogger(options: LoggerOptions = {}): ILogger {
    const childContextName = options.contextName
      ? `${this.contextName}][${options.contextName.replaceAll(/^\[|\]$/g, "")}`
      : this.contextName;
    return new Logger(childContextName, options.logLevel ?? this.logLevel);
  }

  debug(message: string, ...args: any[]): void {
    // Debug logs are eliminated in production builds via build-time substitution
    if (this.isMinLevel(LogLevel.DEBUG)) {
      console.debug(`[${this.contextName}] ${message}`, ...args);
    }
  }

  info(message: string, ...args: any[]): void {
    // Info logs are eliminated in production builds via build-time substitution
    if (this.isMinLevel(LogLevel.INFO)) {
      console.info(`[${this.contextName}] ${message}`, ...args);
    }
  }

  warn(message: string, ...args: any[]): void {
    if (this.isMinLevel(LogLevel.WARN)) {
      console.warn(`[${this.contextName}] ${message}`, ...args);
    }
  }

  error(message: string, ...args: any[]): void {
    if (this.isMinLevel(LogLevel.ERROR)) {
      console.error(`[${this.contextName}] ${message}`, ...args);
    }
  }
}
