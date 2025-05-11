# Instructions for Updating bridge3 Example

This document outlines the necessary modifications to `examples/simple-hello/bridge3/bridge-server.go` and `examples/simple-hello/bridge3/bridge-final.test.ts` to ensure correct envelope handling, flag usage, and stream management for both unary and bidirectional streaming RPCs.

## 1. `examples/simple-hello/bridge3/bridge-server.go` Updates

The primary goal is to ensure all server responses (both success and error for unary; data and end_stream for streaming) are correctly enveloped and that client flags are interpreted as intended.

### 1.1. Unary Call Handling (Server-Side)

- **Location:** `StreamHandler` function, `if isUnary { ... }` block.
- **Request Processing:**
  - The initial envelope from the client for a unary call (`requestEnvelope`) should have `Flags: protocol.FlagNone`. Log a warning if `requestEnvelope.Flags` is different (e.g., `protocol.FlagEndStream`) but proceed with processing the data. Example log: `log.Printf("StreamHandler: WARNING - Unary call received with unexpected flags %d. Expected FlagNone. Processing data anyway. Procedure: %s", requestEnvelope.Flags, procedure)`
- **Response Processing (Success):**
  - When `rec.Code == http.StatusOK`, the `responseDataBytes` (protobuf message) is wrapped in an envelope: `finalResponseEnvelope := &protocol.Envelope{Flags: protocol.FlagNone, Data: responseDataBytes}`.
  - Serialize this envelope: `serializedResponse, err := protocol.SerializeEnvelope(finalResponseEnvelope)`. Handle potential serialization errors (e.g., log and close stream).
- **Response Processing (Error):**
  - When `rec.Code != http.StatusOK` (i.e., the Connect handler returns an error):
    - The `responseDataBytes` (which contains the JSON error string from Connect) **must** be wrapped in a `protocol.Envelope`.
    - This envelope **must** have `Flags: protocol.FlagNone`. Example: `finalResponseEnvelope := &protocol.Envelope{Flags: protocol.FlagNone, Data: responseDataBytes}`.
    - Serialize this envelope: `serializedResponse, err := protocol.SerializeEnvelope(finalResponseEnvelope)`. Handle potential serialization errors.
- **Logging (Crucial for Debugging - Apply to both Success and Error paths for Unary):**
  - Before writing the `serializedResponse` to the `stream`, add a detailed log:
    `log.Printf("StreamHandler: Sending unary response envelope (Flags: %d, DataLen: %d, TotalLen: %d, HexPreview: %x)", finalResponseEnvelope.Flags, len(finalResponseEnvelope.Data), len(serializedResponse), serializedResponse[:min(len(serializedResponse), 32)])`
    (Ensure a `min` helper function is available or defined if not already, e.g., `func min(a, b int) int { if a < b { return a }; return b }`).

### 1.2. Streaming Call Handling (Server-Side)

- **Location:** `StreamHandler` function, `else { // Streaming ... }` block.

- **Request Goroutine (Client -> Server Pipe):**

  - This is the goroutine: `go func() { defer pw.Close() ... }()`.
  - **Initial Envelope:** The first envelope from the client (`requestEnvelope`) should ideally have `Flags: protocol.FlagNone`.
    - If `requestEnvelope.Flags` has `protocol.FlagEndStream` set on this _first_ data envelope:
      - Log this as unexpected client behavior: `log.Printf("StreamHandler: WARNING - Client sent EndStream flag on initial data envelope for streaming call. Procedure: %s", procedure)`.
      - If `len(requestEnvelope.Data) > 0`, write `requestEnvelope.Data` to `pw`.
      - Then, `return` from the goroutine (which defers `pw.Close()`), effectively ending the input to the Connect handler.
  - **Subsequent Envelopes:** For subsequent envelopes read in the loop (`for { env, err := protocol.ParseEnvelope(streamReader) ... }`):
    - Data envelopes should have `Flags: protocol.FlagNone`. Log a warning if different but process data: `log.Printf("StreamHandler: WARNING - Client stream envelope received with unexpected flags %d. Expected FlagNone. Processing data. Procedure: %s", env.Flags, procedure)`
    - If `env.Flags` has `protocol.FlagEndStream` set:
      - `log.Printf("StreamHandler: Received EndStream flag from client. Procedure: %s. DataLen: %d", procedure, len(env.Data))`
      - If `len(env.Data) > 0`, write `env.Data` to the pipe `pw`.
      - Then, `return` from this goroutine (which will `defer pw.Close()`), signaling EOF to the Connect handler.
  - **Logging:** Add logs to show flags of received client envelopes: `log.Printf("StreamHandler: Received client stream envelope (Flags: %d, DataLen: %d). Procedure: %s", env.Flags, len(env.Data), procedure)`. This log should be inside the loop for subsequent envelopes.

- **Response Goroutine (Server Pipe -> Client):**
  - This is the goroutine: `go func() { ... for { select { case <-ctx.Done(): ... case env, ok := <-responsePipeReader: ... } } }()`.
  - Envelopes read from `responsePipeReader` (which are `protocol.Envelope` structs from `customResponseWriter`) should be serialized using `protocol.SerializeEnvelope(env)` before being written to the libp2p `stream`.
  - **Logging (Crucial for Debugging):** Before writing the `serializedMessage` to the `stream`, add a detailed log:
    `log.Printf("StreamHandler: Sending stream response envelope (Flags: %d, DataLen: %d, TotalLen: %d, HexPreview: %x)", env.Flags, len(env.Data), len(serializedMessage), serializedMessage[:min(len(serializedMessage), 32)])`

## 2. `examples/simple-hello/bridge3/bridge-final.test.ts` Updates

The client needs to correctly send flags, parse all responses as envelopes, and manage stream lifecycle.
(Ensure `Flag` enum, `Envelope` type, `parseEnvelope`, `serializeEnvelope`, `toHex`, `uint8ArrayToString`, `encode`, `decode`, and proto message types like `SayHelloResponse`, `BidiStreamingEchoRequest`, `BidiStreamingEchoResponse` are correctly imported/defined).

### 2.1. Unary Test (`SayHello`):

- **Request Sending:**
  - Ensure the `requestEnvelope` for the unary call is created with `flags: Flag.NONE`.
    `const requestEnvelope: Envelope = { flags: Flag.NONE, data: requestData };`
  - Update the log message:
    `console.log(\`[test script] Unary Test: Sending procedure path '\${procedurePath}', Content-Type '\${contentType}', Flags: \${requestEnvelope.flags}, Data (hex): \${toHex(requestEnvelope.data)}\`);`
- **Response Handling:**
  - The response `responseBuffer` from `await pipe(streamToTest, async function* (source) { ... yield await source.next() ... })` must be parsed as an envelope.
    `const parsedResponseEnvelope = parseEnvelope(responseBuffer.subarray()); // Assuming responseBuffer is Uint8Array from pipe`
  - Log received envelope:
    `console.log(\`[test script] Unary Test: Received response envelope. Flags: \${parsedResponseEnvelope.flags}, Data (hex): \${toHex(parsedResponseEnvelope.data)}\`);`
  - **Revised Logic for Unary Response (Server sends `Flag.NONE` for both success and error payloads):**
    ```typescript
    if (parsedResponseEnvelope.flags === Flag.NONE) {
      try {
        // Attempt to decode as successful response
        const responseMessage = decode(
          parsedResponseEnvelope.data,
          SayHelloResponse,
        );
        console.log(
          "[test script] Unary Test: Successfully decoded SayHelloResponse:",
          responseMessage,
        );
        expect(responseMessage.greeting).toBe("Hello, Libp2p User from Go!");
      } catch (decodeError) {
        // If decode fails, assume it's a Connect error JSON payload
        try {
          const errorString = uint8ArrayToString(parsedResponseEnvelope.data);
          const errorData = JSON.parse(errorString);
          console.error(
            "[test script] Unary Test: Received Connect error object:",
            errorData,
          );
          // Check for specific error code if needed, e.g., if (errorData.code === 'not_found')
          fail(
            `Unary test failed: Server returned a Connect error: ${JSON.stringify(errorData)}`,
          );
        } catch (jsonError) {
          console.error(
            "[test script] Unary Test: Failed to decode as SayHelloResponse and also failed to parse as JSON error. Data (hex):",
            toHex(parsedResponseEnvelope.data),
            "Decode Error:",
            decodeError,
            "JSON Error:",
            jsonError,
          );
          fail(
            "Unary test failed: Response data was not a valid SayHelloResponse nor a JSON error.",
          );
        }
      }
    } else {
      fail(
        `Unary test failed: Expected response envelope with Flag.NONE, got flags: ${parsedResponseEnvelope.flags}. Data (hex): ${toHex(parsedResponseEnvelope.data)}`,
      );
    }
    ```

### 2.2. Bidirectional Streaming Test (`BidiStreamingEcho`):

- **Request Sending:**
  - Inside the loop sending messages: `for (let i = 0; i < messagesToSend; i++) { ... }`
    - Each `requestData` (serialized `BidiStreamingEchoRequest`) must be wrapped in an envelope with `flags: Flag.NONE`.
      `const requestEnvelope: Envelope = { flags: Flag.NONE, data: requestData };`
    - Log the sent envelope:
      `console.log(\`[test script] Bidi Test: Sending message \${i + 1}, Flags: \${requestEnvelope.flags}, Data (hex): \${toHex(requestEnvelope.data)}\`);`
    - Write the serialized envelope: `await streamToTest.write(serializeEnvelope(requestEnvelope));`
  - **After the loop:**
    - Call `await streamToTest.closeWrite();` to signal the end of the client's request stream.
    - `console.log('[test script] Bidi Test: Called stream.closeWrite()');`
    - **Do NOT** send an explicit envelope with `Flag.END_STREAM` from the client here. `closeWrite()` handles signaling this to the remote.
- **Response Handling:**
  - Initialize `let receivedMessagesCount = 0;` and `let endStreamReceived = false;`
  - Loop through received chunks: `for await (const chunk of streamToTest.source) { ... }`
    - Parse `chunk` as an envelope: `const receivedEnvelope = parseEnvelope(chunk as Uint8Array);`
    - Log received envelope details: `console.log(\`[test script] Bidi Test: Received envelope. Flags: \${receivedEnvelope.flags}, Data (hex): \${toHex(receivedEnvelope.data)}\`);`
    - If `receivedEnvelope.flags === Flag.END_STREAM`:
      - `console.log('[test script] Bidi Test: Received END_STREAM flag from server.');`
      - `endStreamReceived = true;`
      - If `receivedEnvelope.data && receivedEnvelope.data.length > 0`:
        `console.warn('[test script] Bidi Test: WARNING - Server sent END_STREAM flag with data. Data (hex):', toHex(receivedEnvelope.data));`
      - `break; // Exit the loop as stream is ended by server`
    - Else if `receivedEnvelope.flags === Flag.NONE`:
      - Decode `receivedEnvelope.data` as `BidiStreamingEchoResponse`.
        `const responseMessage = decode(receivedEnvelope.data, BidiStreamingEchoResponse);`
      - `console.log(\`[test script] Bidi Test: Received data message: \${responseMessage.text}\`);`
      - Assert its content: `expect(responseMessage.text).toBe(\`Echo: Message \${receivedMessagesCount + 1}\`);`
      - `receivedMessagesCount++;`
    - Else (unexpected flags):
      - `fail(\`[test script] Bidi Test: Received unexpected flags \${receivedEnvelope.flags}. Data (hex): \${toHex(receivedEnvelope.data)}\`);`
      - `break;`
- **Assertions after loop:**
  - `expect(receivedMessagesCount).toBe(messagesToSend);`
  - `expect(endStreamReceived).toBe(true); // Verify server correctly sent END_STREAM`
  - `console.log('[test script] Bidi Test: All messages received and stream ended correctly.');`

## 3. General Notes & Helper Functions (for `bridge-final.test.ts`)

- Ensure `Flag` enum, `Envelope` type, `parseEnvelope`, `serializeEnvelope` are correctly defined or imported from `../envelope`.
- Ensure `toHex`, `uint8ArrayToString` helper functions are available.
- Ensure `encode` (e.g., `SayHelloRequest.toBinary()`) and `decode` (e.g., `SayHelloResponse.fromBinary()`) methods for your protobuf messages are used correctly.
- Import all necessary protobuf message types (`SayHelloRequest`, `SayHelloResponse`, `BidiStreamingEchoRequest`, `BidiStreamingEchoResponse`) from their generated locations.
