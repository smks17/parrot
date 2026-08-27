export interface Env {
  DB: D1Database;
}

function cors(res: Response): Response {
  res.headers.set("Access-Control-Allow-Origin", "*");
  res.headers.set("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
  res.headers.set("Access-Control-Allow-Headers", "Content-Type");
  return res;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    if (request.method === "OPTIONS") {
      return cors(new Response(null, { status: 204 }));
    }

    const url = new URL(request.url);

    if (url.pathname === "/health") {
      return cors(Response.json({ ok: true }));
    }

    if (url.pathname === "/sessions" && request.method === "POST") {
      const buffer = await request.arrayBuffer();
      if (buffer.byteLength === 0) {
        return cors(Response.json({ error: "empty body" }, { status: 400 }));
      }
      const id = crypto.randomUUID();
      await env.DB.prepare(
        "INSERT INTO sessions (id, snapshot, updated_at) VALUES (?, ?, ?)"
      )
        .bind(id, buffer, Date.now())
        .run();
      return cors(Response.json({ id }));
    }

    const sessionMatch = url.pathname.match(/^\/sessions\/([\w-]+)$/);
    if (sessionMatch && request.method === "GET") {
      const row = await env.DB.prepare(
        "SELECT snapshot FROM sessions WHERE id = ?"
      )
        .bind(sessionMatch[1])
        .first<{ snapshot: ArrayBuffer }>();
      if (!row) {
        return cors(Response.json({ error: "not found" }, { status: 404 }));
      }
      const bytes = new Uint8Array(row.snapshot as unknown as ArrayLike<number>);
      return cors(
        new Response(bytes, {
          headers: { "Content-Type": "application/octet-stream" },
        })
      );
    }

    return cors(Response.json({ error: "not found" }, { status: 404 }));
  },
};
