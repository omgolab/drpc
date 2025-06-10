const drpc_tag = "drpc";

export const config = {
    discovery: {
        tag: drpc_tag,
        pubsubTopic: `${drpc_tag}._peer-discovery._p2p._pubsub`
    },
    drpcProtocolId: `/${drpc_tag}/1.0.0`,
    drpcWebstreamProtocolId: `/${drpc_tag}-webstream/1.0.0`,
};
