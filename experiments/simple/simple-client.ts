import { createLibp2p } from "libp2p";
import { tcp } from "@libp2p/tcp";
import { noise } from "@chainsafe/libp2p-noise";
import { multiaddr } from "@multiformats/multiaddr";
import { mdns } from "@libp2p/mdns";
import { yamux } from "@chainsafe/libp2p-yamux";
import { tls } from "@libp2p/tls";

const PROTOCOL_ID = "/hello/1.0.0";

// Array of messages to send in sequence
const MESSAGES_TO_SEND = [
  "Hello from TypeScript node",
  "This is message #2 from the array",
  "And here is the final message #3",
];

async function main() {
  // Check if address was provided as command line argument
  const peerAddrStr = process.argv[2] || "";
  if (!peerAddrStr) {
    console.log("Usage: bun ts-hello-client.ts <peer-multiaddr>");
    console.log(
      "Example: bun ts-hello-client.ts /ip4/127.0.0.1/tcp/9000/p2p/QmHash",
    );
    console.log("Trying mDNS discovery instead...");
  }

  // Create a libp2p node with TCP and WebSockets enabled
  const node = await createLibp2p({
    addresses: {
      listen: ["/ip4/0.0.0.0/tcp/0"],
    },
    transports: [tcp()],
    connectionEncrypters: [noise(), tls()],
    streamMuxers: [yamux()],
    peerDiscovery: [
      mdns({
        interval: 1000,
      }),
    ],
  });

  // Start the node
  await node.start();
  console.log("TypeScript node started with ID:", node.peerId.toString());

  // If a peer address was provided, connect to it directly
  if (peerAddrStr) {
    try {
      // Ensure the address has the proper format
      let peerAddrStr2 = peerAddrStr;

      // Check if the address is missing the 'ip4' prefix
      if (
        peerAddrStr.startsWith("/127.0.0.1") ||
        peerAddrStr.startsWith("/localhost")
      ) {
        peerAddrStr2 = `/ip4${peerAddrStr}`;
        console.log("Reformatted address to:", peerAddrStr2);
      }

      const peerAddr = multiaddr(peerAddrStr2);
      console.log("🔌 Connecting to peer:", peerAddrStr2);
      await node.dial(peerAddr);

      console.log("🔗 Opening stream with protocol:", PROTOCOL_ID);
      const stream = await node.dialProtocol(peerAddr, PROTOCOL_ID);

      // Send each message in the array
      for (let i = 0; i < MESSAGES_TO_SEND.length; i++) {
        const message = MESSAGES_TO_SEND[i];
        console.log(`📤 SENDING [${i}]: "${message}"`);
        await stream.sink([new TextEncoder().encode(message)]);

        // Read the response after each message
        const responses: string[] = [];
        try {
          for await (const chunk of stream.source) {
            const response = new TextDecoder().decode(chunk.subarray());
            responses.push(response);
            console.log(`📥 RECEIVED RESPONSE [${i}]: "${response}"`);
          }
        } catch (err: any) {
          if (
            err.code !== "ERR_STREAM_RESET" &&
            !String(err).includes("aborted")
          ) {
            console.error(`❌ Error reading response for message ${i}:`, err);
          } else {
            console.log(
              `⚠️ Stream was reset or closed by the server after message ${i}`,
            );
          }
        }

        // Need to create a new stream for each message since the server closes it
        if (i < MESSAGES_TO_SEND.length - 1) {
          console.log("🔄 Creating new stream for next message");
          await stream
            .close()
            .catch((e) => console.error("Error closing stream:", e));
          // Short delay to ensure proper stream closure
          await new Promise((resolve) => setTimeout(resolve, 100));
          const newStream = await node.dialProtocol(peerAddr, PROTOCOL_ID);
          stream.source = newStream.source;
          stream.sink = newStream.sink;
        }
      }

      console.log("✅ All messages sent, closing stream");
      await stream
        .close()
        .catch((e) => console.error("Error in final stream close:", e));
    } catch (err) {
      console.error("Error connecting to peer:", err);
    }
  } else {
    // Otherwise, discover peers via mDNS
    console.log("🔍 Discovering peers via mDNS...");

    node.addEventListener("peer:discovery", async (evt) => {
      const peer = evt.detail;
      console.log("🔍 Discovered peer:", peer.id.toString());

      try {
        await node.dial(peer.id);
        console.log("🔌 Connected to peer:", peer.id.toString());

        // First stream creation
        console.log("🔗 Opening initial stream with protocol:", PROTOCOL_ID);
        let stream = await node.dialProtocol(peer.id, PROTOCOL_ID);

        // Send each message in the array
        for (let i = 0; i < MESSAGES_TO_SEND.length; i++) {
          const message = MESSAGES_TO_SEND[i];
          console.log(`📤 SENDING [${i}]: "${message}"`);
          await stream.sink([new TextEncoder().encode(message)]);

          // Read the response after each message
          const responses: string[] = [];
          try {
            for await (const chunk of stream.source) {
              const response = new TextDecoder().decode(chunk.subarray());
              responses.push(response);
              console.log(`📥 RECEIVED RESPONSE [${i}]: "${response}"`);
            }
          } catch (err: any) {
            if (
              err.code !== "ERR_STREAM_RESET" &&
              !String(err).includes("aborted")
            ) {
              console.error(`❌ Error reading response for message ${i}:`, err);
            } else {
              console.log(
                `⚠️ Stream was reset or closed by the server after message ${i}`,
              );
            }
          }

          // Need to create a new stream for each message since the server closes it
          if (i < MESSAGES_TO_SEND.length - 1) {
            console.log("🔄 Creating new stream for next message");
            await stream
              .close()
              .catch((e) => console.error("Error closing stream:", e));
            // Short delay to ensure proper stream closure
            await new Promise((resolve) => setTimeout(resolve, 100));
            stream = await node.dialProtocol(peer.id, PROTOCOL_ID);
          }
        }

        console.log("✅ All messages sent, closing stream");
        await stream
          .close()
          .catch((e) => console.error("Error in final stream close:", e));
      } catch (err) {
        console.error("❌ Failed to connect to peer:", err);
      }
    });
  }

  // Wait until the user terminates the process
  console.log("Press Ctrl+C to exit");
  process.on("SIGINT", async () => {
    console.log("Shutting down...");
    await node.stop();
    process.exit(0);
  });
}

main().catch((err) => {
  console.error("Error in main:", err);
  process.exit(1);
});
