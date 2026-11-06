// Slack's MCP server and authorization server on one origin, as e2e/slack_fixtures_test.go has
// them: no registration_endpoint, a token endpoint that answers HTTP 200 {"ok": ...}, single-use
// refresh tokens. Authorize redirects at once, as a user clicking Allow would. Contract:
// e2e/slack-mcp-contract.md.
import { createHash } from "node:crypto";
import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";

export const slackClientID = "fake-slack-client-1.1";
export const slackScopes = [
  "search:read.public",
  "channels:history",
  "chat:write",
  "users:read",
];

export type Slack = {
  url: string;
  config: { ttl: number; reject: boolean };
  authorizes: URLSearchParams[];
  // Every token request: its form and whether it carried an Authorization header.
  exchanges: { form: URLSearchParams; authorization?: string }[];
  refreshes: { form: URLSearchParams; authorization?: string }[];
  tokens: () => { access: string; refresh: string };
  close: () => Promise<void>;
};

export async function startSlack(): Promise<Slack> {
  let origin = "";
  let issued = 0;
  let access = "";
  let refresh = "";
  const codes = new Map<string, { challenge: string; redirect: string }>();
  const s: Slack = {
    url: "",
    config: { ttl: 43200, reject: false },
    authorizes: [],
    exchanges: [],
    refreshes: [],
    tokens: () => ({ access, refresh }),
    close: () => new Promise((ok) => server.close(() => ok())),
  };
  const json = (res: ServerResponse, status: number, body: unknown) => {
    res
      .writeHead(status, { "Content-Type": "application/json" })
      .end(JSON.stringify(body));
  };
  const issue = (res: ServerResponse) => {
    issued++;
    access = `fake-slack-access-ui-${issued}`;
    refresh = `fake-slack-refresh-ui-${issued}`;
    json(res, 200, {
      ok: true,
      access_token: access,
      refresh_token: refresh,
      token_type: "user",
      expires_in: s.config.ttl,
      scope: slackScopes.join(","),
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
        scopes_supported: slackScopes,
      });
    } else if (url.pathname === "/.well-known/oauth-authorization-server") {
      json(res, 200, {
        issuer: origin,
        authorization_endpoint: `${origin}/oauth/v2_user/authorize`,
        token_endpoint: `${origin}/api/oauth.v2.user.access`,
        code_challenge_methods_supported: ["S256"],
        response_types_supported: ["code"],
        grant_types_supported: ["authorization_code", "refresh_token"],
        token_endpoint_auth_methods_supported: ["none"],
      });
    } else if (url.pathname === "/oauth/v2_user/authorize") {
      const q = url.searchParams;
      s.authorizes.push(q);
      const redirect = q.get("redirect_uri") ?? "";
      if (
        q.get("client_id") !== slackClientID ||
        q.get("code_challenge_method") !== "S256" ||
        !redirect.startsWith("http://127.0.0.1:")
      ) {
        res.writeHead(400).end();
        return;
      }
      const code = `fake-slack-code-ui-${s.authorizes.length}`;
      codes.set(code, { challenge: q.get("code_challenge") ?? "", redirect });
      res
        .writeHead(302, {
          Location: `${redirect}?code=${code}&state=${q.get("state")}`,
        })
        .end();
    } else if (
      url.pathname === "/api/oauth.v2.user.access" &&
      req.method === "POST"
    ) {
      const form = new URLSearchParams(body);
      const seen = { form, authorization: req.headers.authorization };
      if (
        form.has("client_secret") ||
        req.headers.authorization ||
        form.get("client_id") !== slackClientID
      ) {
        json(res, 200, { ok: false, error: "invalid_client" });
        return;
      }
      if (form.get("grant_type") === "refresh_token") {
        s.refreshes.push(seen);
        if (s.config.reject || form.get("refresh_token") !== refresh) {
          json(res, 200, { ok: false, error: "invalid_refresh_token" });
          return;
        }
        issue(res);
        return;
      }
      const grant = codes.get(form.get("code") ?? "");
      codes.delete(form.get("code") ?? "");
      const challenge = createHash("sha256")
        .update(form.get("code_verifier") ?? "")
        .digest("base64url");
      if (
        !grant ||
        grant.redirect !== form.get("redirect_uri") ||
        grant.challenge !== challenge
      ) {
        json(res, 200, { ok: false, error: "invalid_code" });
        return;
      }
      s.exchanges.push(seen);
      issue(res);
    } else if (url.pathname === "/mcp" && req.method === "POST") {
      if (!access || req.headers.authorization !== `Bearer ${access}`) {
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
              serverInfo: { name: "fake-slack", version: "0" },
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
              ? { content: [{ type: "text", text: "slack-answer-42" }] }
              : {};
      json(res, 200, { jsonrpc: "2.0", id: rpc.id, result });
    } else {
      res.writeHead(404).end();
    }
  });
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok));
  const addr = server.address();
  origin = `http://127.0.0.1:${typeof addr === "object" && addr ? addr.port : 0}`;
  s.url = `${origin}/mcp`;
  return s;
}
