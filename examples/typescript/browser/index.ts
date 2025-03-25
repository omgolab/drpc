import { createPromiseClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { GreeterService } from "../src/greeter_connect.js";
import { SayHelloRequest, StreamingEchoRequest } from "../src/greeter_pb.js";

async function main() {
  // Get peer info from the server
  const peerInfoResp = await fetch("http://localhost:8080/p2pinfo");
  const peerInfo = await peerInfoResp.json();
  console.log("Peer info:", peerInfo);

  console.log("\n=== Scenario 1: Direct libp2p connection ===");
  let transport = createConnectTransport({
    baseUrl: "http://localhost:8080",
    useBinaryFormat: true,
    useHttpGet: true,
  });
  let client = createPromiseClient(GreeterService, transport);

  // Test unary call via direct libp2p
  console.log("Testing unary call via direct libp2p...");
  try {
    const request = new SayHelloRequest({ name: "Direct libp2p" });
    const resp = await client.sayHello(request);
    console.log("Response:", resp.message);
  } catch (err) {
    console.error("Direct libp2p error:", err);
  }

  // Test streaming via direct libp2p
  console.log("Testing streaming via direct libp2p...");
  try {
    const request = new StreamingEchoRequest({
      message: "Direct libp2p stream",
    });
    const stream = await client.streamingEcho(
      (async function* () {
        yield request;
      })(),
    );
    for await (const response of stream) {
      console.log("Received:", response.message);
    }
  } catch (err) {
    console.error("Direct libp2p streaming error:", err);
  }

  console.log("\n=== Scenario 2: HTTP Connect-RPC connection ===");
  transport = createConnectTransport({
    baseUrl: "http://localhost:8080",
    useBinaryFormat: true,
    useHttpGet: true,
  });
  client = createPromiseClient(GreeterService, transport);

  // Test unary call via HTTP Connect-RPC
  console.log("Testing unary call via HTTP Connect-RPC...");
  try {
    const request = new SayHelloRequest({ name: "HTTP Connect" });
    const resp = await client.sayHello(request);
    console.log("Response:", resp.message);
  } catch (err) {
    console.error("HTTP Connect error:", err);
  }

  // Test streaming via HTTP Connect-RPC
  console.log("Testing streaming via HTTP Connect-RPC...");
  try {
    const request = new StreamingEchoRequest({
      message: "HTTP Connect stream",
    });
    const stream = await client.streamingEcho(
      (async function* () {
        yield request;
      })(),
    );
    for await (const response of stream) {
      console.log("Received:", response.message);
    }
  } catch (err) {
    console.error("HTTP Connect streaming error:", err);
  }

  console.log("\n=== Scenario 3: Connect-RPC Gateway connection ===");
  const gatewayPath = `/@/${peerInfo.Addrs[0].slice(1)}/@`;
  console.log("Using gateway path:", gatewayPath);
  const gatewayTransport = createConnectTransport({
    baseUrl: `http://localhost:8080${gatewayPath}`,
    useBinaryFormat: true,
    useHttpGet: true,
  });
  const gatewayClient = createPromiseClient(GreeterService, gatewayTransport);

  // Test unary call via gateway
  console.log("Testing unary call via gateway...");
  try {
    const request = new SayHelloRequest({ name: "Gateway" });
    const resp = await gatewayClient.sayHello(request);
    console.log("Response:", resp.message);
  } catch (err) {
    console.error("Gateway error:", err);
  }

  // Test streaming via gateway
  console.log("Testing streaming via gateway...");
  try {
    const request = new StreamingEchoRequest({ message: "Gateway stream" });
    const stream = await gatewayClient.streamingEcho(
      (async function* () {
        yield request as any;
      })(),
    );
    for await (const response of stream) {
      console.log("Received:", response.message);
    }
  } catch (err) {
    console.error("Gateway streaming error:", err);
  }
}

main().catch(console.error);
