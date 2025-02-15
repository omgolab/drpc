import { createConnectTransport } from "@connectrpc/connect-web";
import { createPromiseClient } from "@connectrpc/connect";
import { GreeterService } from "../src/greeter_connect.js";
import { SayHelloRequest, SayHelloResponse } from "../src/greeter_pb.js";

async function main() {
  // Fetch peer info from /p2pinfo
  let peerInfo;
  try {
    const response = await fetch("http://localhost:8080/p2pinfo");
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    peerInfo = await response.json();
  } catch (e) {
    console.error("Failed to fetch peer info.  Please ensure the server is running with HTTP enabled, and that you are using the correct port.", e);
    return; // Exit if we can't get peer info
  }

  // Example 1: Direct HTTP connection
  console.log("Testing direct HTTP connection...");
  const directTransport = createConnectTransport({
    baseUrl: "http://localhost:8080",
  });
  const directClient = createPromiseClient(GreeterService, directTransport);
  try {
    const request = new SayHelloRequest({
      name: "Direct HTTP"
    });
    const directResponse = await directClient.sayHello(request);
    console.log("Direct HTTP response:", directResponse?.message);
  } catch (e) {
    console.error("Direct HTTP error:", e);
  }

  // Example 2: Gateway connection to p2p node
  console.log("\nTesting p2p gateway connection...");

  // Check if peerInfo and Addrs are available
  if (!peerInfo || !peerInfo.Addrs || peerInfo.Addrs.length === 0) {
    console.error("Peer info is missing or invalid.  Please ensure the server is running, and that you are using the correct port.");
    return;
  }

  // Correctly construct the gateway URL.  The address should already include /p2p/<peerid>
  const gatewayUrl = `http://localhost:8080/@/${peerInfo.Addrs[0]}/@/greeter.v1.GreeterService`;
  const gatewayTransport = createConnectTransport({
    baseUrl: gatewayUrl,
  });
  const gatewayClient = createPromiseClient(GreeterService, gatewayTransport);
  try {
    const request = new SayHelloRequest({
      name: "P2P Gateway"
    });
    const gatewayResponse = await gatewayClient.sayHello(request);
    console.log("P2P Gateway response:", gatewayResponse?.message);
  } catch (e) {
    console.error("P2P Gateway error:", e);
  }
}

main().catch((e) => console.error("Main error:", e));