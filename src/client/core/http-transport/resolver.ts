/**
 * HTTP to libp2p resolution logic
 */
import { ConnectError, Code } from "@connectrpc/connect";
import { multiaddr } from "@multiformats/multiaddr";
import { getCachedP2pAddr, cacheP2pAddr } from "./cache-manager";
import { DRPCOptions } from "../types";

/**
 * Resolve HTTP URL to libp2p multiaddress via /p2pinfo endpoint
 */
export async function resolveHttpToP2p(
    originalHttpUrl: string,
    clientOptions: DRPCOptions,
): Promise<string> {
    const logger = clientOptions.logger.createChildLogger({
        contextName: "HTTP-Resolver",
    });

    let p2pAddr = getCachedP2pAddr(originalHttpUrl);
    if (!p2pAddr) {
        const p2pInfoUrl = new URL(originalHttpUrl);
        // Ensure the path is exactly /p2pinfo, respecting potential existing base paths
        const baseHostnameAndPort = `${p2pInfoUrl.protocol}//${p2pInfoUrl.host}`;
        const finalP2pInfoUrl = `${baseHostnameAndPort}/p2pinfo`;

        logger.debug(`[Resolver] Fetching p2pinfo from ${finalP2pInfoUrl}`);
        try {
            const response = await fetch(finalP2pInfoUrl.toString());
            if (!response.ok) {
                const errorText = await response.text();
                throw new ConnectError(
                    `Failed to fetch /p2pinfo: ${response.status} ${response.statusText}. Body: ${errorText}`,
                    Code.Unavailable,
                );
            }
            const data = await response.json();
            if (
                !data.Addrs ||
                !Array.isArray(data.Addrs) ||
                data.Addrs.length === 0
            ) {
                throw new ConnectError(
                    "Invalid /p2pinfo response: missing or empty 'Addrs' array",
                    Code.DataLoss,
                );
            }

            let selectedAddr: string | undefined = undefined;
            let firstP2PAddr: string | undefined = undefined;

            // Always prefer local addresses for local development/testing, even if public addresses are present
            for (const addrStr of data.Addrs) {
                if (typeof addrStr === "string" && addrStr.includes("/p2p/")) {
                    try {
                        multiaddr(addrStr); // Validate if it's a proper multiaddr

                        if (!firstP2PAddr) {
                            firstP2PAddr = addrStr; // Keep track of the first valid p2p address
                        }

                        // Always prefer local addresses for local development/testing
                        if (
                            addrStr.startsWith("/ip4/127.0.0.1/") ||
                            addrStr.startsWith("/ip6/::1/")
                        ) {
                            selectedAddr = addrStr;
                            logger.debug(
                                `[Resolver] Preferred local p2p address from /p2pinfo: ${selectedAddr}`,
                            );
                            break; // Found a local address, use it
                        }
                    } catch (e) {
                        logger.warn(
                            `[Resolver] Invalid multiaddr string in /p2pinfo Addrs array: "${addrStr}"`,
                            e,
                        );
                    }
                }
            }

            // If no local address was found, use the first valid P2P address encountered
            if (!selectedAddr && firstP2PAddr) {
                selectedAddr = firstP2PAddr;
                logger.debug(
                    `[Resolver] No local p2p address found, using first available: ${selectedAddr}`,
                );
            }

            if (!selectedAddr) {
                throw new ConnectError(
                    "No suitable p2p multiaddress found in /p2pinfo response's 'Addrs' array",
                    Code.DataLoss,
                );
            }
            p2pAddr = selectedAddr;
            cacheP2pAddr(originalHttpUrl, p2pAddr);
            logger.debug(
                `[Resolver] Resolved ${originalHttpUrl} to ${p2pAddr} and cached.`,
            );
        } catch (error: any) {
            console.error(
                `[Resolver] Error fetching or parsing /p2pinfo for ${originalHttpUrl}:`,
                error,
            );
            if (error instanceof ConnectError) throw error;
            throw new ConnectError(
                `Failed to resolve p2p address for ${originalHttpUrl} via /p2pinfo: ${error.message}`,
                Code.Unavailable,
            );
        }
    } else {
        logger.debug(
            `[Resolver] Using cached p2p address ${p2pAddr} for ${originalHttpUrl}`,
        );
    }

    return p2pAddr;
}
