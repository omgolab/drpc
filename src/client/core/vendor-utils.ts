/**
 * Vendor utilities imported from connect-es for code optimization
 * Source: docs/vendors/connect-es/packages/connect/src/protocol-connect/code-string.ts
 */
import { Code, ConnectError } from "@connectrpc/connect";

/**
 * codeToString returns the string representation of a Code.
 *
 * @private Internal code, does not follow semantic versioning.
 */
export function codeToString(value: Code): string {
    const name = Code[value] as string | undefined;
    if (typeof name != "string") {
        return value.toString();
    }
    return (
        name[0].toLowerCase() +
        name.substring(1).replace(/[A-Z]/g, (c) => "_" + c.toLowerCase())
    );
}

let stringToCode: Record<string, Code> | undefined;

/**
 * codeFromString parses the string representation of a Code in snake_case.
 * For example, the string "permission_denied" parses into Code.PermissionDenied.
 *
 * If the given string cannot be parsed, the function returns undefined.
 *
 * @private Internal code, does not follow semantic versioning.
 */
export function codeFromString(value: string | undefined): Code | undefined {
    if (value === undefined) {
        return undefined;
    }
    if (!stringToCode) {
        stringToCode = {};
        for (const value of Object.values(Code)) {
            if (typeof value == "string") {
                continue;
            }
            stringToCode[codeToString(value)] = value;
        }
    }
    return stringToCode[value];
}

/**
 * Parse a Connect error from JSON bytes.
 * Source: docs/vendors/connect-es/packages/connect/src/protocol-connect/error-json.ts
 * @private Internal code, does not follow semantic versioning.
 */
// export function errorFromJsonBytes(
//     bytes: Uint8Array,
//     metadata: HeadersInit | undefined,
//     fallback: ConnectError,
// ): ConnectError {
//     let jsonValue: any;
//     try {
//         jsonValue = JSON.parse(new TextDecoder().decode(bytes));
//     } catch (e) {
//         throw fallback;
//     }

//     if (
//         typeof jsonValue !== "object" ||
//         jsonValue == null ||
//         Array.isArray(jsonValue)
//     ) {
//         throw fallback;
//     }

//     let code = fallback.code;
//     if ("code" in jsonValue && typeof jsonValue.code === "string") {
//         code = codeFromString(jsonValue.code) ?? code;
//     }

//     const message = jsonValue.message;
//     if (message != null && typeof message !== "string") {
//         throw fallback;
//     }

//     return new ConnectError(message ?? "", code, metadata);
// }

/**
 * Efficient buffer concatenation using vendor approach.
 * This optimizes our custom concatUint8Arrays implementation.
 */
export function concatUint8Arrays(arrays: Uint8Array[]): Uint8Array {
    if (arrays.length === 0) {
        return new Uint8Array(0);
    }
    if (arrays.length === 1) {
        return arrays[0];
    }

    // Calculate total length
    const totalLength = arrays.reduce((acc, array) => acc + array.byteLength, 0);

    // Create new array and copy data efficiently
    const result = new Uint8Array(totalLength);
    let offset = 0;

    for (const array of arrays) {
        result.set(array, offset);
        offset += array.byteLength;
    }

    return result;
}
