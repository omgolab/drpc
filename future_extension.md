# Future Extension: ConnectRPC/gRPC Plugin

This document outlines a plan for creating a ConnectRPC/gRPC plugin that automates the generation of necessary files for using dRPC with ConnectRPC/gRPC services.

## Goals

*   Automate the creation of client and server code for dRPC transport.
*   Provide a seamless integration with existing ConnectRPC/gRPC workflows.
*   Reduce boilerplate code and improve developer experience.

## Functionality

The plugin should:

1.  **Generate Go code:**
    *   Generate `client.go` and `server.go` files with dRPC transport implementation.
    *   Generate necessary code for creating and configuring libp2p host and listener.
    *   Generate code for the gateway middleware.
2.  **Generate TypeScript code:**
    *   Generate `greeter_connect.ts` and `greeter_pb.ts` files with ConnectRPC client implementation.
    *   Generate a minimal TS file (`index.ts`) to use the gateway and run it via bun.
3.  **Configuration:**
    *   Allow users to configure the plugin via command-line flags or a configuration file (e.g., `drpc.yaml`).
    *   Configuration options should include:
        *   libp2p peer ID
        *   Listen address
        *   Protocol ID
        *   Output directory

## Implementation

1.  **Use `protoc` and `protoc-gen-go`:**
    *   Leverage the existing `protoc` and `protoc-gen-go` tools for generating Go code from protobuf definitions.
    *   Create a custom `protoc-gen-drpc` plugin that extends the functionality of `protoc-gen-go`.
2.  **Use `protoc-gen-connect-es`:**
    *   Leverage the existing `protoc-gen-connect-es` tools for generating TypeScript code from protobuf definitions.
    *   Create a custom plugin or modify the existing plugin to generate the required dRPC client code.
3.  **Code Generation Templates:**
    *   Use code generation templates to generate the Go and TypeScript code.
    *   Templates should be flexible and customizable to allow users to modify the generated code.

## Workflow

1.  **Define protobuf service:**
    *   The user defines a ConnectRPC/gRPC service using protobuf.
2.  **Configure the plugin:**
    *   The user configures the `protoc-gen-drpc` plugin using command-line flags or a configuration file.
3.  **Generate code:**
    *   The user runs `protoc` with the `protoc-gen-drpc` plugin to generate the Go and TypeScript code.
4.  **Implement the service:**
    *   The user implements the service logic in the generated Go code.
5.  **Run the service:**
    *   The user runs the generated Go code to start the ConnectRPC/gRPC service with dRPC transport.

## Future Considerations

*   Support for other programming languages (e.g., Python, Java).
*   Integration with other transport protocols (e.g., WebSockets).
*   Automatic discovery of dRPC services.