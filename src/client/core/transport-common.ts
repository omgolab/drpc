/**
 * Common utilities and patterns shared between transport handlers
 * 
 * This module consolidates shared code patterns between http-transport and libp2p-transport
 * to reduce duplication and improve maintainability.
 */
import { ConnectError, Code } from "@connectrpc/connect";
import {
    createClientMethodSerializers,
    createLinkedAbortController,
} from "@connectrpc/connect/protocol";
import { ILogger } from "./logger";
import type { DescMessage, DescMethodUnary, DescMethodStreaming } from "@bufbuild/protobuf";

/**
 * Shared serializer cache to avoid repeated createClientMethodSerializers calls
 * Used by both unary and stream handlers across all transports
 */
const globalSerializerCache = new Map<string, any>();

/**
 * Content type helper to determine serialization format
 */
export interface ContentTypeOptions {
    contentType: string;
}

/**
 * Determines if content type should use binary format (proto) vs JSON format
 */
export function shouldUseBinaryFormat(contentType: string): boolean {
    return !contentType.includes("json");
}

/**
 * Creates or retrieves cached serializers for a method
 * 
 * @param method - The protobuf method descriptor (unary or streaming)
 * @param useBinaryFormat - Whether to use binary (proto) or JSON format
 * @param logger - Logger instance for debug output
 * @returns Cached or newly created serializers
 */
export function getCachedSerializers(
    method: any,
    useBinaryFormat: boolean,
    logger: ILogger,
): any {
    const cacheKey = `${method.parent.typeName}.${method.name}:${useBinaryFormat}`;
    
    let serializers = globalSerializerCache.get(cacheKey);
    if (!serializers) {
        serializers = createClientMethodSerializers(method, useBinaryFormat);
        globalSerializerCache.set(cacheKey, serializers);
        logger.debug(`Created and cached serializers for ${cacheKey}`);
    } else {
        logger.debug(`Using cached serializers for ${cacheKey}`);
    }
    
    return serializers;
}

/**
 * Creates a linked abort controller with proper error handling
 * Wrapper around Connect's createLinkedAbortController with enhanced logging
 */
export function createManagedAbortController(
    signal: AbortSignal | undefined,
    logger: ILogger,
    context: string,
): AbortController {
    const controller = createLinkedAbortController(signal);
    
    if (signal?.aborted) {
        logger.debug(`${context}: Request already aborted`);
    }
    
    // Add logging for abort events
    controller.signal.addEventListener('abort', () => {
        logger.debug(`${context}: Request aborted`);
    });
    
    return controller;
}

/**
 * Common error creation patterns used across transports
 */
export class TransportErrorFactory {
    /**
     * Create a connection error with consistent formatting
     */
    static createConnectionError(
        message: string,
        cause?: Error,
        code: Code = Code.Unavailable,
    ): ConnectError {
        const errorMessage = cause ? `${message}: ${cause.message}` : message;
        return new ConnectError(errorMessage, code, undefined, undefined, cause);
    }

    /**
     * Create a timeout error with consistent formatting
     */
    static createTimeoutError(
        timeoutMs: number,
        operation: string,
    ): ConnectError {
        return new ConnectError(
            `${operation} timed out after ${timeoutMs}ms`,
            Code.DeadlineExceeded,
        );
    }

    /**
     * Create a data loss error for empty responses
     */
    static createEmptyResponseError(context: string): ConnectError {
        return new ConnectError(
            `Empty response received from server in ${context}`,
            Code.DataLoss,
        );
    }

    /**
     * Create an aborted error when request is cancelled
     */
    static createAbortedError(context: string): ConnectError {
        return new ConnectError(
            `Request aborted by client in ${context}`,
            Code.Canceled,
        );
    }

    /**
     * Create an invalid content type error with consistent formatting
     */
    static createInvalidContentTypeError(
        contentType: string,
        validTypes: readonly string[],
        callType: string,
    ): ConnectError {
        return new ConnectError(
            `Invalid content type for ${callType} call: ${contentType}. Must be one of: ${validTypes.join(", ")}`,
            Code.InvalidArgument,
        );
    }
}

/**
 * Buffer processing utilities shared across transports
 */
export class BufferUtils {
    /**
     * Validates that chunk arrays are not empty and contain valid data
     */
    static validateChunks(chunks: Uint8Array[], context: string): void {
        if (chunks.length === 0) {
            throw TransportErrorFactory.createEmptyResponseError(context);
        }
        
        const validChunks = chunks.filter(chunk => chunk.length > 0);
        if (validChunks.length === 0) {
            throw TransportErrorFactory.createEmptyResponseError(context);
        }
    }

    /**
     * Creates debug information for chunks
     */
    static getChunkDebugInfo(chunks: Uint8Array[]): string {
        const totalBytes = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
        const chunkSizes = chunks.map(chunk => chunk.length);
        return `${chunks.length} chunks, ${totalBytes} total bytes, sizes: [${chunkSizes.join(', ')}]`;
    }
}

/**
 * Common connection utilities for libp2p transport handlers
 */

/**
 * Common connection utilities for libp2p transport handlers
 */
export class Libp2pConnectionManager {
    /**
     * Establishes a connection to a target multiaddress with consistent logging
     */
    static async dialProtocol(
        libp2p: any,
        ma: any,
        protocolId: string,
        signal: AbortSignal,
        logger: ILogger,
        context: string,
    ): Promise<any> {
        logger.debug(`${context}: Dialing ${ma.toString()} with protocol ${protocolId}`);
        
        const stream = await libp2p.dialProtocol(ma, protocolId, {
            signal,
        });
        
        logger.debug(`Connected to ${ma.toString()} via ${protocolId}`);
        
        return stream;
    }
}

/**
 * Content type validation utilities
 */
export class ContentTypeValidator {
    /**
     * Validates unary content type and throws consistent error if invalid
     */
    static validateUnaryContentType(
        contentType: string,
        validTypes: readonly string[],
    ): void {
        if (!validTypes.includes(contentType)) {
            throw TransportErrorFactory.createInvalidContentTypeError(
                contentType,
                validTypes,
                'unary'
            );
        }
    }

    /**
     * Validates streaming content type and throws consistent error if invalid  
     */
    static validateStreamingContentType(
        contentType: string,
        validTypes: readonly string[],
    ): void {
        if (!validTypes.includes(contentType)) {
            throw TransportErrorFactory.createInvalidContentTypeError(
                contentType,
                validTypes,
                'streaming'
            );
        }
    }
}

/**
 * Clear the serializer cache (useful for testing or memory cleanup)
 */
export function clearSerializerCache(): void {
    globalSerializerCache.clear();
}

/**
 * Get serializer cache stats (useful for debugging)
 */
export function getSerializerCacheStats(): { size: number; keys: string[] } {
    return {
        size: globalSerializerCache.size,
        keys: Array.from(globalSerializerCache.keys()),
    };
}
