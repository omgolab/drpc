// Copyright 2021-2025 The Connect Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import type {
  DescMessage,
  MessageInitShape,
  MessageShape,
  DescMethodStreaming,
  DescMethodUnary,
} from "@bufbuild/protobuf";
import { requestHeaderWithCompression } from "./request-header.js";
import { validateResponseWithCompression } from "./validate-response.js";
import { validateTrailer } from "./validate-trailer.js";
import { Code } from "../code.js";
import { ConnectError } from "../connect-error.js";
import type {
  UnaryResponse,
  UnaryRequest,
  StreamResponse,
  StreamRequest,
} from "../interceptor.js";
import {
  pipe,
  createAsyncIterable,
  transformSerializeEnvelope,
  transformCompressEnvelope,
  transformJoinEnvelopes,
  pipeTo,
  transformSplitEnvelope,
  transformDecompressEnvelope,
  transformParseEnvelope,
} from "../protocol/async-iterable.js";
import { createMethodUrl } from "../protocol/create-method-url.js";
import { runUnaryCall, runStreamingCall } from "../protocol/run-call.js";
import { createMethodSerializationLookup } from "../protocol/serialization.js";
import type { CommonTransportOptions } from "../protocol/transport-options.js";
import type { Transport } from "../transport.js";
import { createContextValues } from "../context-values.js";
import type { ContextValues } from "../context-values.js";
import { headerGrpcStatus } from "./headers.js";

/**
 * Create a Transport for the gRPC protocol.
 */
export function createTransport(opt: CommonTransportOptions): Transport {
  return {
    async unary<I extends DescMessage, O extends DescMessage>(
      method: DescMethodUnary<I, O>,
      signal: AbortSignal | undefined,
      timeoutMs: number | undefined,
      header: HeadersInit | undefined,
      message: MessageInitShape<I>,
      contextValues?: ContextValues,
    ): Promise<UnaryResponse<I, O>> {
      const serialization = createMethodSerializationLookup(
        method,
        opt.binaryOptions,
        opt.jsonOptions,
        opt,
      );
      timeoutMs =
        timeoutMs === undefined
          ? opt.defaultTimeoutMs
          : timeoutMs <= 0
            ? undefined
            : timeoutMs;
      return await runUnaryCall<I, O>({
        interceptors: opt.interceptors,
        signal,
        timeoutMs,
        req: {
          stream: false,
          service: method.parent,
          method,
          requestMethod: "POST",
          url: createMethodUrl(opt.baseUrl, method),
          header: requestHeaderWithCompression(
            opt.useBinaryFormat,
            timeoutMs,
            header,
            opt.acceptCompression,
            opt.sendCompression,
          ),
          contextValues: contextValues ?? createContextValues(),
          message,
        },
        next: async (req: UnaryRequest<I, O>): Promise<UnaryResponse<I, O>> => {
          const uRes = await opt.httpClient({
            url: req.url,
            method: "POST",
            header: req.header,
            signal: req.signal,
            body: pipe(
              createAsyncIterable([req.message]),
              transformSerializeEnvelope(
                serialization.getI(opt.useBinaryFormat),
              ),
              transformCompressEnvelope(
                opt.sendCompression,
                opt.compressMinBytes,
              ),
              transformJoinEnvelopes(),
              {
                propagateDownStreamError: true,
              },
            ),
          });
          const { compression, headerError } = validateResponseWithCompression(
            opt.acceptCompression,
            uRes.status,
            uRes.header,
          );
          const message = await pipeTo(
            uRes.body,
            transformSplitEnvelope(opt.readMaxBytes),
            transformDecompressEnvelope(compression ?? null, opt.readMaxBytes),
            transformParseEnvelope<MessageShape<O>>(
              serialization.getO(opt.useBinaryFormat),
            ),
            async (iterable) => {
              let message: MessageShape<O> | undefined;
              for await (const chunk of iterable) {
                if (message !== undefined) {
                  throw new ConnectError(
                    "protocol error: received extra output message for unary method",
                    Code.Unimplemented,
                  );
                }
                message = chunk;
              }
              return message;
            },
            { propagateDownStreamError: false },
          );
          validateTrailer(uRes.trailer, uRes.header);
          if (message === undefined) {
            // Trailers only response
            if (headerError) {
              throw headerError;
            }
            throw new ConnectError(
              "protocol error: missing output message for unary method",
              uRes.trailer.has(headerGrpcStatus)
                ? Code.Unimplemented
                : Code.Unknown,
            );
          }
          if (headerError) {
            throw new ConnectError(
              "protocol error: received output message for unary method with error status",
              Code.Unknown,
            );
          }
          return <UnaryResponse<I, O>>{
            stream: false,
            service: method.parent,
            method,
            header: uRes.header,
            message,
            trailer: uRes.trailer,
          };
        },
      });
    },
    async stream<I extends DescMessage, O extends DescMessage>(
      method: DescMethodStreaming<I, O>,
      signal: AbortSignal | undefined,
      timeoutMs: number | undefined,
      header: HeadersInit | undefined,
      input: AsyncIterable<MessageInitShape<I>>,
      contextValues?: ContextValues,
    ): Promise<StreamResponse<I, O>> {
      const serialization = createMethodSerializationLookup(
        method,
        opt.binaryOptions,
        opt.jsonOptions,
        opt,
      );
      timeoutMs =
        timeoutMs === undefined
          ? opt.defaultTimeoutMs
          : timeoutMs <= 0
            ? undefined
            : timeoutMs;
      return runStreamingCall<I, O>({
        interceptors: opt.interceptors,
        signal,
        timeoutMs,
        req: {
          stream: true,
          service: method.parent,
          method,
          requestMethod: "POST",
          url: createMethodUrl(opt.baseUrl, method),
          header: requestHeaderWithCompression(
            opt.useBinaryFormat,
            timeoutMs,
            header,
            opt.acceptCompression,
            opt.sendCompression,
          ),
          contextValues: contextValues ?? createContextValues(),
          message: input,
        },
        next: async (req: StreamRequest<I, O>) => {
          const uRes = await opt.httpClient({
            url: req.url,
            method: "POST",
            header: req.header,
            signal: req.signal,
            body: pipe(
              req.message,
              transformSerializeEnvelope(
                serialization.getI(opt.useBinaryFormat),
              ),
              transformCompressEnvelope(
                opt.sendCompression,
                opt.compressMinBytes,
              ),
              transformJoinEnvelopes(),
              { propagateDownStreamError: true },
            ),
          });
          const { compression, foundStatus, headerError } =
            validateResponseWithCompression(
              opt.acceptCompression,
              uRes.status,
              uRes.header,
            );
          if (headerError) {
            throw headerError;
          }
          const res: StreamResponse<I, O> = {
            ...req,
            header: uRes.header,
            trailer: uRes.trailer,
            message: pipe(
              uRes.body,
              transformSplitEnvelope(opt.readMaxBytes),
              transformDecompressEnvelope(
                compression ?? null,
                opt.readMaxBytes,
              ),
              transformParseEnvelope(serialization.getO(opt.useBinaryFormat)),
              async function* (iterable) {
                yield* iterable;
                if (!foundStatus) {
                  validateTrailer(uRes.trailer, uRes.header);
                }
              },
              { propagateDownStreamError: true },
            ),
          };
          return res;
        },
      });
    },
  };
}
