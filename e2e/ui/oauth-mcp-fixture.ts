// A hosted MCP server that needs sign-in, as e2e/mcp_oauth_fixtures_test.go has it, on one origin:
// the 401 names the protected-resource metadata, which names the auth server (the same origin).
// Authorize redirects at once, as a user clicking Allow would. Contract: e2e/mcp-oauth-contract.md.
import { createHash } from "node:crypto";
import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";

export type OAuthMCP = {
  url: string;
  // registration: advertise registration_endpoint; refuse: it answers 403, as Figma's does for an
  // unapproved client; requireAuth: false answers every request.
  config: {
    registration: boolean;
    refuse: boolean;
    requireAuth: boolean;
    ttl: number;
    reject: boolean;
  };
  registrations: {
    redirect_uris: string[];
    token_endpoint_auth_method: string;
  }[];
  authorizes: URLSearchParams[];
  refreshes: URLSearchParams[];
  tokens: () => { access: string; refresh: string };
  close: () => Promise<void>;
};

export async function startOAuthMCP(): Promise<OAuthMCP> {
  let origin = "";
  let issued = 0;
  let access = "";
  let refresh = "";
  const clients = new Map<string, string>();
  const codes = new Map<
    string,
    { client: string; challenge: string; redirect: string }
  >();
  const o: OAuthMCP = {
    url: "",
    config: {
      registration: true,
      refuse: false,
      requireAuth: true,
      ttl: 7200,
      reject: false,
    },
    registrations: [],
    authorizes: [],
    refreshes: [],
    tokens: () => ({ access, refresh }),
    close: () => new Promise((ok) => server.close(() => ok())),
  };
  const json = (res: ServerResponse, status: number, body: unknown) => {
    res
      .writeHead(status, { "Content-Type": "application/json" })
      .end(JSON.stringify(body));
  };
  const issue = (res: ServerResponse, ttl: number) => {
    issued++;
    access = `fake-oauth-access-ui-${issued}`;
    refresh = `fake-oauth-refresh-ui-${issued}`;
    json(res, 200, {
      access_token: access,
      refresh_token: refresh,
      token_type: "Bearer",
      expires_in: ttl,
    });
  };
  const read = (req: IncomingMessage) =>
    new Promise<string>((ok) => {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => ok(body));
    });

  const server: Server = createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", origin);
    const body = await read(req);
    if (url.pathname === "/.well-known/oauth-protected-resource") {
      json(res, 200, {
        resource: `${origin}/mcp`,
        authorization_servers: [origin],
      });
    } else if (url.pathname === "/.well-known/oauth-authorization-server") {
      json(res, 200, {
        issuer: origin,
        authorization_endpoint: `${origin}/authorize`,
        token_endpoint: `${origin}/token`,
        ...(o.config.registration && {
          registration_endpoint: `${origin}/register`,
        }),
        code_challenge_methods_supported: ["S256"],
        response_types_supported: ["code"],
        grant_types_supported: ["authorization_code", "refresh_token"],
        token_endpoint_auth_methods_supported: ["none"],
      });
    } else if (url.pathname === "/register" && req.method === "POST") {
      if (o.config.refuse) {
        json(res, 403, { error: "Forbidden" });
        return;
      }
      const reg = JSON.parse(body);
      o.registrations.push(reg);
      const client = `fake-oauth-client-ui-${o.registrations.length}`;
      clients.set(client, reg.redirect_uris[0]);
      json(res, 201, { client_id: client, redirect_uris: reg.redirect_uris });
    } else if (url.pathname === "/authorize") {
      const q = url.searchParams;
      o.authorizes.push(q);
      const redirect = clients.get(q.get("client_id") ?? "");
      if (
        !redirect ||
        q.get("redirect_uri") !== redirect ||
        q.get("code_challenge_method") !== "S256"
      ) {
        res.writeHead(400).end();
        return;
      }
      const code = `fake-oauth-code-ui-${o.authorizes.length}`;
      codes.set(code, {
        client: q.get("client_id")!,
        challenge: q.get("code_challenge") ?? "",
        redirect,
      });
      res
        .writeHead(302, {
          Location: `${redirect}?code=${code}&state=${q.get("state")}`,
        })
        .end();
    } else if (url.pathname === "/token" && req.method === "POST") {
      const form = new URLSearchParams(body);
      if (form.get("grant_type") === "refresh_token") {
        o.refreshes.push(form);
        if (o.config.reject || form.get("refresh_token") !== refresh) {
          json(res, 400, { error: "invalid_grant" });
          return;
        }
        issue(res, o.config.ttl);
        return;
      }
      const grant = codes.get(form.get("code") ?? "");
      codes.delete(form.get("code") ?? "");
      const challenge = createHash("sha256")
        .update(form.get("code_verifier") ?? "")
        .digest("base64url");
      if (
        !grant ||
        grant.client !== form.get("client_id") ||
        grant.challenge !== challenge
      ) {
        json(res, 400, { error: "invalid_grant" });
        return;
      }
      issue(res, o.config.ttl);
    } else if (url.pathname === "/mcp" && req.method === "POST") {
      if (
        o.config.requireAuth &&
        (!access || req.headers.authorization !== `Bearer ${access}`)
      ) {
        res
          .writeHead(401, {
            "WWW-Authenticate": `Bearer resource_metadata="${origin}/.well-known/oauth-protected-resource"`,
          })
          .end();
        return;
      }
      const rpc = JSON.parse(body || "{}");
      if (rpc.id === undefined) {
        res.writeHead(202).end();
        return;
      }
      const result =
        rpc.method === "initialize"
          ? {
              protocolVersion: "2025-06-18",
              capabilities: { tools: {} },
              serverInfo: { name: "fake-oauth", version: "0" },
            }
          : rpc.method === "tools/list"
            ? {
                tools: [
                  {
                    name: "answer",
                    description: "Answers a fixed text.",
                    inputSchema: { type: "object", properties: {} },
                  },
                ],
              }
            : rpc.method === "tools/call"
              ? { content: [{ type: "text", text: "oauth-answer-42" }] }
              : {};
      json(res, 200, { jsonrpc: "2.0", id: rpc.id, result });
    } else {
      res.writeHead(404).end();
    }
  });
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok));
  const addr = server.address();
  origin = `http://127.0.0.1:${typeof addr === "object" && addr ? addr.port : 0}`;
  o.url = `${origin}/mcp`;
  return o;
}

// followAuthorize is the browser step done over HTTP: it follows the authorize URL Connect returned to
// Reef's localhost callback.
export async function followAuthorize(authorizeURL: string) {
  const hop = await fetch(authorizeURL, { redirect: "manual" });
  const location = hop.headers.get("location");
  if (hop.status !== 302 || !location)
    throw new Error(`authorize answered ${hop.status}`);
  const cb = await fetch(location);
  if (cb.status !== 200) throw new Error(`callback answered ${cb.status}`);
}
