# @connectrpc/connect-next

Connect is a family of libraries for building and consuming APIs on different languages and platforms, and
[@connectrpc/connect](https://www.npmjs.com/package/@connectrpc/connect) brings type-safe APIs with Protobuf to
TypeScript.

`@connectrpc/connect-next` provides a plugin for [Next.js](https://nextjs.org/),
the React Framework for the Web.

### nextJsApiRouter()

Provide your Connect RPCs via Next.js API routes. To enable Connect in Next.js,
add two files to your project:

```diff
.
├── connect.ts
└── pages
    └── api
        └── [[...connect]].ts
```

> **Note:** Next.js 13 introduced the new App Router. Your Connect API routes
> need to be placed in `pages/`, but you can use the `app/` directory for the
> App Router at the same time.

The new file `connect.ts` is where you register your RPCs:

```ts
// connect.ts
import { ConnectRouter } from "@connectrpc/connect";

export default function (router: ConnectRouter) {
  // implement rpc Say(SayRequest) returns (SayResponse)
  router.rpc(ElizaService, ElizaService.methods.say, async (req) => ({
    sentence: `you said: ${req.sentence}`,
  }));
}
```

`pages/api/[[..connect]].ts` is a Next.js [catch-all API route](https://nextjs.org/docs/routing/dynamic-routes#catch-all-routes):

```ts
// pages/api/[[..connect]].ts
import { nextJsApiRouter } from "@connectrpc/connect-next";
import routes from "../../connect";

const { handler, config } = nextJsApiRouter({ routes });
export { handler as default, config };
```

With that server running, you can make requests with any Connect or gRPC-Web client.
Note that Next.js serves all your RPCs with the `/api` prefix.

`curl` with the Connect protocol:

```bash
curl \
    --header "Content-Type: application/json" \
    --data '{"sentence": "I feel happy."}' \
    --http2-prior-knowledge \
    http://localhost:3000/api/connectrpc.eliza.v1.ElizaService/Say
```

Node.js with the gRPC-web protocol (using a transport from [@connectrpc/connect-node](https://www.npmjs.com/package/@connectrpc/connect-node)):

```ts
import { createClient } from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-node";
import { ElizaService } from "./gen/eliza_connect.js";

const transport = createGrpcWebTransport({
  baseUrl: "http://localhost:3000/api",
  httpVersion: "1.1",
});

const client = createClient(ElizaService, transport);
const { sentence } = await client.say({ sentence: "I feel happy." });
console.log(sentence); // you said: I feel happy.
```

A client for the web browser actually looks identical to this example - it would
simply use `createConnectTransport` from [@connectrpc/connect-web](https://www.npmjs.com/package/@connectrpc/connect-web)
instead.

Note that support for gRPC is limited, since many gRPC clients require HTTP/2,
and Express does not support the Node.js `http2` module.

### Deploying to Vercel

Currently, `@connectrpc/connect-next` does not support the Vercel Edge runtime.
It requires the Node.js server runtime, which is used by default when deploying
to Vercel.

## Getting started

To get started with Connect, head over to the [docs](https://connectrpc.com/docs/node/getting-started)
for a tutorial, or take a look at [our example](https://github.com/connectrpc/connect-es/tree/main/packages/example).
