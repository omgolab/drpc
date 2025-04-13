# DRPC System Patterns

## Architecture Overview

DRPC follows a client-server architecture with the following components:

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

## Design Patterns

- **Interface-based design** - Services are defined via interfaces
- **Middleware pattern** - For authentication, logging, error handling
- **Factory pattern** - For creating clients and servers
- **Observer pattern** - For handling streaming events
- **Decorator pattern** - For adding features like retry, timeout

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
