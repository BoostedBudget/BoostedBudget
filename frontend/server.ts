/**
 * Bun development server with file watching and live reload
 * Migrated from Vite in Issue #575
 *
 * Features:
 * - File watching with automatic rebuild
 * - API proxy to backend (/api, /health)
 * - HTTPS support for Tailscale access (optional)
 * - Dynamic port configuration via environment variables
 * - Static file serving from dist/
 */

import { watch } from "fs";
import { readFileSync, existsSync } from "fs";
import { join, extname } from "path";
import tailwindPlugin from "bun-plugin-tailwind";

const port = parseInt(
  process.env.PORT || process.env.E2E_FRONTEND_PORT || "3000"
);
const backendPort = process.env.E2E_BACKEND_PORT || "8080";
const isDocker = !!process.env.DOCKER_ENV;

// Check for HTTPS certificates (for Tailscale access)
const certPath = join(import.meta.dir, "cert.pem");
const keyPath = join(import.meta.dir, "key.pem");
const hasHttps = existsSync(certPath) && existsSync(keyPath);

const srcDir = join(import.meta.dir, "src");
const distDir = join(import.meta.dir, "dist");
const publicDir = join(import.meta.dir, "public");

// MIME types for static file serving
const mimeTypes: Record<string, string> = {
  ".html": "text/html",
  ".js": "application/javascript",
  ".mjs": "application/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".svg": "image/svg+xml",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".map": "application/json",
};

// Build the application
async function build() {
  console.log("\n[dev] Building...");
  const startTime = Date.now();

  // Clean dist
  const { rmSync, mkdirSync, cpSync } = await import("fs");
  if (existsSync(distDir)) {
    rmSync(distDir, { recursive: true });
  }
  mkdirSync(distDir, { recursive: true });

  const result = await Bun.build({
    entrypoints: [join(srcDir, "main.tsx")],
    outdir: distDir,
    minify: false, // Don't minify for dev
    sourcemap: "linked",
    target: "browser",
    splitting: true,
    plugins: [tailwindPlugin],
    define: {
      "process.env.NODE_ENV": JSON.stringify("development"),
    },
    naming: {
      entry: "[name].[ext]",
      chunk: "[name]-[hash].[ext]",
      asset: "[name]-[hash].[ext]",
    },
  });

  if (!result.success) {
    console.error("[dev] Build failed:");
    for (const log of result.logs) {
      console.error(log);
    }
    return false;
  }

  // Copy public assets
  if (existsSync(publicDir)) {
    cpSync(publicDir, distDir, { recursive: true });
  }

  // Generate index.html
  const { writeFileSync } = await import("fs");
  const indexHtmlTemplate = readFileSync(
    join(import.meta.dir, "index.html"),
    "utf-8"
  );

  const mainOutput = result.outputs.find(
    (o) => o.kind === "entry-point" && o.path.includes("main")
  );
  const mainScriptPath = mainOutput
    ? "/" + mainOutput.path.replace(distDir + "/", "")
    : "/main.js";

  const cssOutputs = result.outputs.filter((o) => o.path.endsWith(".css"));
  const cssLinks = cssOutputs
    .map((css) => {
      const cssPath = "/" + css.path.replace(distDir + "/", "");
      return `<link rel="stylesheet" href="${cssPath}">`;
    })
    .join("\n    ");

  // Add live reload script for development
  const liveReloadScript = `
    <script>
      // Simple live reload - reconnects on connection loss
      let ws;
      function connect() {
        ws = new WebSocket('ws://localhost:${port + 1}');
        ws.onmessage = () => location.reload();
        ws.onclose = () => setTimeout(connect, 1000);
      }
      connect();
    </script>
  `;

  let indexHtml = indexHtmlTemplate
    .replace(
      '<script type="module" src="/src/main.tsx"></script>',
      `${cssLinks ? cssLinks + "\n    " : ""}<script type="module" src="${mainScriptPath}"></script>${liveReloadScript}`
    )
    .replace('href="/vite.svg"', 'href="/favicon.svg"');

  writeFileSync(join(distDir, "index.html"), indexHtml);

  const elapsed = Date.now() - startTime;
  console.log(`[dev] Build completed in ${elapsed}ms`);
  return true;
}

// Determine backend URL based on environment
const getBackendUrl = (pathname: string, search: string) => {
  const host = isDocker ? "backend:8080" : `localhost:${backendPort}`;
  return `http://${host}${pathname}${search}`;
};

// Proxy handler for API requests
async function proxyToBackend(req: Request): Promise<Response> {
  const url = new URL(req.url);
  const backendUrl = getBackendUrl(url.pathname, url.search);

  try {
    const response = await fetch(backendUrl, {
      method: req.method,
      headers: req.headers,
      body: req.body,
      // @ts-expect-error - Bun supports duplex streaming
      duplex: "half",
    });

    const headers = new Headers(response.headers);
    headers.set("Access-Control-Allow-Origin", "*");

    return new Response(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
  } catch (error) {
    console.error(`[dev] Proxy error for ${url.pathname}:`, error);
    return new Response(JSON.stringify({ error: "Backend unavailable" }), {
      status: 502,
      headers: { "Content-Type": "application/json" },
    });
  }
}

// Serve static files
function serveStatic(pathname: string): Response | null {
  // Try exact path in dist
  let filePath = join(distDir, pathname);
  if (existsSync(filePath) && !Bun.file(filePath).size) {
    filePath = join(distDir, pathname, "index.html");
  }

  if (!existsSync(filePath)) {
    return null;
  }

  try {
    const file = Bun.file(filePath);
    const ext = extname(filePath);
    const contentType = mimeTypes[ext] || "application/octet-stream";

    return new Response(file, {
      headers: {
        "Content-Type": contentType,
        "Cache-Control": "no-cache",
      },
    });
  } catch {
    return null;
  }
}

// TLS configuration for HTTPS (optional)
const tls = hasHttps
  ? {
      key: readFileSync(keyPath),
      cert: readFileSync(certPath),
    }
  : undefined;

// Live reload WebSocket server
let wsClients: Set<any> = new Set();

const wsServer = Bun.serve({
  port: port + 1,
  fetch(req, server) {
    if (server.upgrade(req)) {
      return;
    }
    return new Response("Upgrade required", { status: 426 });
  },
  websocket: {
    open(ws) {
      wsClients.add(ws);
    },
    close(ws) {
      wsClients.delete(ws);
    },
    message() {},
  },
});

function notifyReload() {
  for (const ws of wsClients) {
    try {
      ws.send("reload");
    } catch {
      wsClients.delete(ws);
    }
  }
}

// Initial build
await build();

// Watch for file changes
let debounceTimer: ReturnType<typeof setTimeout> | null = null;

const watcher = watch(srcDir, { recursive: true }, async (event, filename) => {
  if (!filename) return;

  // Debounce rapid changes
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(async () => {
    console.log(`[dev] File changed: ${filename}`);
    const success = await build();
    if (success) {
      notifyReload();
    }
  }, 100);
});

// Start the main server
const server = Bun.serve({
  port,
  hostname: "0.0.0.0",
  tls,

  async fetch(req) {
    const url = new URL(req.url);

    // Proxy /api and /health requests to backend
    if (url.pathname.startsWith("/api") || url.pathname.startsWith("/health")) {
      return proxyToBackend(req);
    }

    // Handle CORS preflight
    if (req.method === "OPTIONS") {
      return new Response(null, {
        headers: {
          "Access-Control-Allow-Origin": "*",
          "Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
          "Access-Control-Allow-Headers": "Content-Type, Authorization",
        },
      });
    }

    // Try to serve static file
    const staticResponse = serveStatic(url.pathname);
    if (staticResponse) {
      return staticResponse;
    }

    // SPA fallback - serve index.html for all other routes
    const indexPath = join(distDir, "index.html");
    if (existsSync(indexPath)) {
      return new Response(Bun.file(indexPath), {
        headers: {
          "Content-Type": "text/html",
          "Cache-Control": "no-cache",
        },
      });
    }

    return new Response("Not Found", { status: 404 });
  },
});

const protocol = hasHttps ? "https" : "http";
console.log(`
  Frontend dev server running at ${protocol}://localhost:${server.port}

  Features:
  - Live reload enabled (WebSocket on port ${port + 1})
  - API proxy: /api -> ${isDocker ? "http://backend:8080" : `http://localhost:${backendPort}`}
  - Health proxy: /health -> ${isDocker ? "http://backend:8080" : `http://localhost:${backendPort}`}
  ${hasHttps ? "- HTTPS enabled (cert.pem/key.pem found)" : "- HTTPS disabled (no certificates found)"}

  Watching for changes in src/...
`);

// Cleanup on exit
process.on("SIGINT", () => {
  watcher.close();
  wsServer.stop();
  server.stop();
  process.exit(0);
});
