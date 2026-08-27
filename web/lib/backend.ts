const BASE = process.env.NEXT_PUBLIC_BACKEND_URL ?? "";

export async function saveSession(bytes: Uint8Array): Promise<string> {
  const body = new Blob([new Uint8Array(bytes)]);
  const res = await fetch(`${BASE}/sessions`, { method: "POST", body });
  if (!res.ok) throw new Error(`saveSession failed: ${res.status}`);
  const { id } = await res.json();
  return id;
}

export async function fetchSession(id: string): Promise<Uint8Array> {
  const res = await fetch(`${BASE}/sessions/${id}`);
  if (!res.ok) throw new Error(`fetchSession failed: ${res.status}`);
  return new Uint8Array(await res.arrayBuffer());
}
