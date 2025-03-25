# dRPC: Build Decentralized Applications with the Simplicity of ConnectRPC

## The Decentralized Future is Calling

The world is rapidly moving towards decentralized applications, driven by the need for greater resilience, security, and user control. But building these applications presents significant challenges. Developers must grapple with complex peer-to-peer networking, secure communication, and efficient data exchange. What if you could leverage the power of a robust P2P network without sacrificing the developer-friendly experience of a modern RPC framework? That's where dRPC comes in.

## Introducing dRPC: ConnectRPC + libp2p

dRPC is a groundbreaking Go library that seamlessly bridges the gap between ConnectRPC and libp2p. It empowers you to build decentralized applications using the familiar and efficient ConnectRPC framework, while harnessing the power of libp2p's peer-to-peer networking capabilities.

## Why dRPC?

dRPC offers a compelling combination of benefits for developers:

- **Effortless RPC:** ConnectRPC provides a streamlined, type-safe way to define and consume APIs using Protocol Buffers. You get all the benefits of code generation, clear contracts, and efficient communication, without the complexity of traditional RPC frameworks.
- **Built-in Decentralization:** libp2p handles the heavy lifting of peer discovery, connection management, and secure communication. Your application becomes inherently decentralized, resilient to network failures, and resistant to censorship.
- **No Vendor Lock-in:** dRPC is built on open standards (ConnectRPC, libp2p, Protocol Buffers). You're not tied to a specific cloud provider or platform.
- **Future-Proof Development:** You're building on a solid foundation of established and actively developed technologies. As ConnectRPC and libp2p evolve, dRPC will evolve with them.
- **HTTP Gateway:** Offers an HTTP gateway for non-libp2p clients.

## Understanding the Core Technologies

- **ConnectRPC:** A modern RPC framework that uses HTTP/2 and Protocol Buffers. It's known for its simplicity, performance, and excellent developer experience. Think of it as a more streamlined and developer-friendly alternative to gRPC.
- **libp2p:** A modular network stack designed for peer-to-peer applications. It provides the building blocks for creating decentralized networks, handling everything from discovering other peers to establishing secure connections.

## Architecture

dRPC's architecture is elegantly simple:

```
[ConnectRPC Client] <-> [libp2p Network] <-> [dRPC Server] <-> [ConnectRPC Service]
```

Also, dRPC server can expose an HTTP gateway:

```
[HTTP Client] <-> [HTTP Gateway] <-> [dRPC Server]
```

Your ConnectRPC client communicates with a dRPC server, which in turn interacts with your ConnectRPC service implementation. The communication between the client and server is handled by libp2p, providing the decentralized and resilient transport layer.

## Use Cases: What Can You Build?

dRPC opens up a world of possibilities for decentralized applications. Here are just a few examples:

- **Decentralized Chat:** Build secure, censorship-resistant chat applications without relying on central servers.
- **Collaborative Editing:** Enable real-time collaboration on documents, code, or other data, with changes propagated directly between peers.
- **Distributed Data Processing:** Create applications that can distribute computational tasks across a network of nodes.
- **Decentralized Social Networks:** Build social platforms that are not controlled by a single entity.
- **Decentralized File Sharing:** Share files directly between peers, without relying on cloud storage providers.

## Getting Started

Getting started with dRPC is straightforward. You'll need Go (1.20 or later) and the `buf` tool for code generation. The core dependencies (ConnectRPC and libp2p) are managed as Go modules.

**Installation:**

```bash
go get github.com/omgolab/drpc
```

Check the project's [README](link to your github readme) for detailed instructions on setting up your project, defining your service with Protocol Buffers, and generating the necessary code.

## The Road Ahead

dRPC is an evolving project, and we have ambitious plans for the future:

- **Enhanced Gateway Functionality:** Improving the reliability and performance of the HTTP gateway.
- **Expanded Language Support:** Exploring support for additional programming languages beyond Go.
- **Integration with Other Transports:** Adding support for transports beyond libp2p, such as WebSockets.
- **Automatic Service Discovery:** Simplifying the process of finding and connecting to dRPC services.
- **Robust Testing:** Expanding the test suite to ensure the highest levels of reliability and stability.

## Join the Movement!

dRPC is an open-source project, and we welcome contributions from the community. Whether you're a seasoned Go developer or just starting your journey into decentralized applications, we encourage you to:

- **Try dRPC:** Experiment with the library and see what you can build.
- **Contribute:** Help us improve dRPC by submitting bug reports, feature requests, or code contributions.
- **Spread the Word:** Share dRPC with your network and help us grow the community.

Let's build the decentralized future together!
