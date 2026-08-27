import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  agentRules: false,
  // Cloudflare Pages serves static files — no Node server behind this app,
  // so Next.js needs to emit plain HTML/JS/CSS instead of expecting a
  // server to render pages on request.
  output: "export",
};

export default nextConfig;
