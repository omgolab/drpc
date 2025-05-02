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
import { validateTrailer } from "../protocol-grpc/validate-trailer.js";
import { requestHeaderWithCompression } from "./request-header.js";
import { validateResponseWithCompression } from "./validate-response.js";
import { createTrailerSerialization, trailerFlag } from "./trailer.js";
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
 * Create a Transport for the gRPC-web protocol.
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
            true,
          ),
          contextValues: contextValues ?? createContextValues(),
          message,
        },
        next: async (req: UnaryRequest<I, O>): Promise<UnaryResponse<I, O>> => {
          const uRes = await opt.httpClient({
            url: req.url,
            method: req.requestMethod,
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
          const { trailer, message } = await pipeTo(
            uRes.body,
            transformSplitEnvelope(opt.readMaxBytes),
            transformDecompressEnvelope(compression ?? null, opt.readMaxBytes),
            transformParseEnvelope<MessageShape<O>, Headers>(
              serialization.getO(opt.useBinaryFormat),
              trailerFlag,
              createTrailerSerialization(),
            ),
            async (iterable) => {
              let message: MessageShape<O> | undefined;
              let trailer: Headers | undefined;
              for await (const env of iterable) {
                if (env.end) {
                  if (trailer !== undefined) {
                    throw new ConnectError(
                      "protocol error: received extra trailer",
                      Code.Unimplemented,
                    );
                  }
                  trailer = env.value;
                } else {
                  if (message !== undefined) {
                    throw new ConnectError(
                      "protocol error: received extra output message for unary method",
                      Code.Unimplemented,
                    );
                  }
                  message = env.value;
                }
              }
              return { trailer, message };
            },
            {
              propagateDownStreamError: false,
            },
          );
          if (trailer === undefined) {
            if (headerError != undefined) {
              throw headerError;
            }
            throw new ConnectError(
              "protocol error: missing trailer",
              uRes.header.has(headerGrpcStatus)
                ? Code.Unimplemented
                : Code.Unknown,
            );
          }
          validateTrailer(trailer, uRes.header);
          if (message === undefined) {
            throw new ConnectError(
              "protocol error: missing output message for unary method",
              trailer.has(headerGrpcStatus) ? Code.Unimplemented : Code.Unknown,
            );
          }
          return <UnaryResponse<I, O>>{
            stream: false,
            service: method.parent,
            method,
            header: uRes.header,
            message,
            trailer,
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
            true,
          ),
          contextValues: contextValues ?? createContextValues(),
          message: input,
        },
        next: async (req: StreamRequest<I, O>) => {
          const uRes = await opt.httpClient({
            url: req.url,
            method: req.requestMethod,
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
            trailer: new Headers(),
            message: pipe(
              uRes.body,
              transformSplitEnvelope(opt.readMaxBytes),
              transformDecompressEnvelope(
                compression ?? null,
                opt.readMaxBytes,
              ),
              transformParseEnvelope(
                serialization.getO(opt.useBinaryFormat),
                trailerFlag,
                createTrailerSerialization(),
              ),
              async function* (iterable) {
                if (foundStatus) {
                  // A grpc-status: 0 response header was present. This is a "trailers-only"
                  // response (a response without a body and no trailers).
                  //
                  // The spec seems to disallow a trailers-only response for status 0 - we are
                  // lenient and only verify that the body is empty.
                  //
                  // > [...] Trailers-Only is permitted for calls that produce an immediate error.
                  // See https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md
                  const r = await iterable[Symbol.asyncIterator]().next();
                  if (r.done !== true) {
                    throw new ConnectError(
                      "protocol error: extra data for trailers-only",
                      Code.InvalidArgument,
                    );
                  }
                  return;
                }
                let trailerReceived = false;
                for await (const chunk of iterable) {
                  if (chunk.end) {
                    if (trailerReceived) {
                      throw new ConnectError(
                        "protocol error: received extra trailer",
                        Code.InvalidArgument,
                      );
                    }
                    trailerReceived = true;
                    validateTrailer(chunk.value, uRes.header);
                    chunk.value.forEach((value, key) =>
                      res.trailer.set(key, value),
                    );
                    continue;
                  }
                  if (trailerReceived) {
                    throw new ConnectError(
                      "protocol error: received extra message after trailer",
                      Code.InvalidArgument,
                    );
                  }
                  yield chunk.value;
                }
                if (!trailerReceived) {
                  throw new ConnectError(
                    "protocol error: missing trailer",
                    Code.Internal,
                  );
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
