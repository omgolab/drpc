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

/**
 * Determine the Connect error code for the given HTTP status code.
 * See https://connectrpc.com/docs/protocol/#http-to-error-code
 *
 * @private Internal code, does not follow semantic versioning.
 */
export function codeFromHttpStatus(httpStatus: number): Code {
  switch (httpStatus) {
    case 400: // Bad Request
      return Code.Internal;
    case 401: // Unauthorized
      return Code.Unauthenticated;
    case 403: // Forbidden
      return Code.PermissionDenied;
    case 404: // Not Found
      return Code.Unimplemented;
    case 429: // Too Many Requests
      return Code.Unavailable;
    case 502: // Bad Gateway
      return Code.Unavailable;
    case 503: // Service Unavailable
      return Code.Unavailable;
    case 504: // Gateway Timeout
      return Code.Unavailable;
    default:
      return Code.Unknown;
  }
}

/**
 * Returns a HTTP status code for the given Connect code.
 * See https://connectrpc.com/docs/protocol#error-codes
 *
 * @private Internal code, does not follow semantic versioning.
 */
export function codeToHttpStatus(code: Code): number {
  switch (code) {
    case Code.Canceled:
      return 499; // Client Closed Request
    case Code.Unknown:
      return 500; // Internal Server Error
    case Code.InvalidArgument:
      return 400; // Bad Request
    case Code.DeadlineExceeded:
      return 504; // Gateway Timeout
    case Code.NotFound:
      return 404; // Not Found
    case Code.AlreadyExists:
      return 409; // Conflict
    case Code.PermissionDenied:
      return 403; // Forbidden
    case Code.ResourceExhausted:
      return 429; // Too Many Requests
    case Code.FailedPrecondition:
      return 400; // Bad Request
    case Code.Aborted:
      return 409; // Conflict
    case Code.OutOfRange:
      return 400; // Bad Request
    case Code.Unimplemented:
      return 501; // Not Implemented
    case Code.Internal:
      return 500; // Internal Server Error
    case Code.Unavailable:
      return 503; // Service Unavailable
    case Code.DataLoss:
      return 500; // Internal Server Error
    case Code.Unauthenticated:
      return 401; // Unauthorized
    default:
      return 500; // same as CodeUnknown
  }
}
