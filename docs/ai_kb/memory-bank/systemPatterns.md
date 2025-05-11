# dRPC System Patterns

## Architecture Overview

dRPC follows a client-server architecture with the following components:

```
┌────────────┐                 ┌────────────┐
│            │    Network      │            │
│   Client   │<───────────────>│   Server   │
│            │                 │            │
└────────────┘                 └────────────┘
```

## Core Components

1. **Protocol** - Defines the message format and communication patterns
2. **Transport** - Handles the network communication layer (HTTP, libp2p). Includes libp2p features like DHT for peer discovery and relay mechanisms (AutoRelay, explicit circuit addresses).
3. **Serialization** - Converts between language-specific types and wire format (likely Protocol Buffers).
4. **Service Definition** - Interface for defining RPC methods.
5. **Client** - Initiates RPC calls to remote services via HTTP, direct libp2p, or relayed libp2p addresses.
6. **Server** - Implements and exposes RPC services. Can listen on HTTP and/or libp2p. Includes logic for dynamic relay peer discovery using DHT with a specific tag (`RELAY_DISCOVERY_TAG`).
7. **ConnectBridge** - Optimized bridge for Connect RPC over libp2p streams. Provides compatibility between Connect RPC and libp2p transports.

## Design Patterns

- **Interface-based design** - Services are defined via interfaces
- **Middleware pattern** - For authentication, logging, error handling
- **Factory pattern** - For creating clients and servers
- **Observer pattern** - For handling streaming events
- **Decorator pattern** - For adding features like retry, timeout
- **Bridge pattern** - Connecting different protocol implementations (Connect RPC and libp2p)
- **Strategy pattern** - Different implementations for handling Connect RPC over libp2p (envelope-aware vs stream transport)
- **Modular client architecture** - Separation of concerns with clear module boundaries

## Communication Patterns

1. **Unary RPC** - Simple request/response
2. **Server Streaming** - Server sends multiple responses
3. **Client Streaming** - Client sends multiple requests
4. **Bidirectional Streaming** - Both sides can send multiple messages

## Error Handling Strategy

- Standardized error codes and messages
- Contextual error information
- Automatic retry mechanisms
- Circuit breaking for failure isolation
- Graceful handling of stream reset errors

## Connect RPC Bridge Architecture

The Connect RPC Bridge is a specialized component that enables efficient communication between Connect RPC services and libp2p transport. It follows these patterns:

```
┌───────────────┐                ┌───────────────┐                ┌───────────────┐
│               │                │               │                │               │
│  Connect RPC  │<───────────────│ ConnectBridge │<───────────────│    libp2p     │
│   Handler     │                │               │                │    Stream     │
│               │                │               │                │               │
└───────────────┘                └───────────────┘                └───────────────┘
```

The bridge offers two implementation strategies:

1. **Envelope-Aware Handler**:

   - Directly parses Connect RPC envelope format
   - Maximum performance with minimal overhead
   - Supports all Connect RPC protocols and content types
   - Handles both unary and streaming RPCs

2. **Stream Transport Handler**:
   - Uses pipes to connect libp2p streams with HTTP handlers
   - Simpler implementation that's easier to understand and extend
   - Compatible with standard Connect RPC HTTP handlers
   - More buffering but easier to debug

## TypeScript Client Architecture

The TypeScript client follows a modular architecture pattern:

```
┌────────────────────────────────────────────────┐
│                 drpc-client.ts                 │
│     (Public API - Thin wrapper around impl)    │
└───────────────────────┬────────────────────────┘
                        │
┌───────────────────────▼────────────────────────┐
│                 client/index.ts                │
│         (Core implementation & routing)        │
└─┬────────────────────┬──────────────────────┬──┘
  │                    │                      │
  ▼                    ▼                      ▼
┌──────────────┐ ┌───────────────┐ ┌────────────────────┐
│ types.ts     │ │ utils.ts      │ │ Other shared code  │
│ (Interfaces) │ │ (Utilities)   │ │                    │
└──────────────┘ └───────────────┘ └────────────────────┘
  │                    │                      │
  ▼                    ▼                      ▼
┌──────────────┐ ┌───────────────┐ ┌────────────────────┐
│http-transport│ │libp2p-transport│ │ Future transports  │
│              │ │                │ │                    │
└──────────────┘ └───────────────┘ └────────────────────┘
```

Key aspects of this architecture:

1. **Clean separation of concerns** - Each module has a specific responsibility
2. **Transport abstraction** - Common interfaces for different transport mechanisms
3. **Shared utilities** - Common code for parsing, serialization, etc.
4. **Thin Public API** - Main entry point remains simple while implementation details are hidden
5. **Transport independence** - New transports can be added without changing the public API
