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

import { Code } from "../code.js";
import { ConnectError } from "../connect-error.js";
import { headerGrpcStatus } from "./headers.js";
import { findTrailerError } from "./trailer-status.js";

/**
 * Validates a trailer for the gRPC and the gRPC-web protocol.
 *
 * If the trailer contains an error status, a ConnectError is
 * thrown. It will include trailer and header in the error's
 * "metadata" property.
 *
 * Throws a ConnectError with code "internal" if neither the trailer
 * nor the header contain the Grpc-Status field.
 *
 * @private Internal code, does not follow semantic versioning.
 */
export function validateTrailer(trailer: Headers, header: Headers): void {
  const err = findTrailerError(trailer);
  if (err) {
    header.forEach((value, key) => {
      err.metadata.append(key, value);
    });
    throw err;
  }
  if (!header.has(headerGrpcStatus) && !trailer.has(headerGrpcStatus)) {
    throw new ConnectError("protocol error: missing status", Code.Internal);
  }
}
