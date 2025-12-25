/**
 * Bun production build script
 * Migrated from Vite in Issue #575
 *
 * Features:
 * - TypeScript/JSX compilation
 * - Tailwind CSS processing via bun-plugin-tailwind
 * - Minification and tree-shaking
 * - Source map generation
 * - Path alias resolution (via tsconfig.json)
 */

import { readFileSync, writeFileSync, mkdirSync, cpSync, existsSync } from "fs";
import { join } from "path";
import tailwindPlugin from "bun-plugin-tailwind";

const outdir = join(import.meta.dir, "dist");

console.log("Building BoostedBudget frontend...\n");

// Clean output directory
if (existsSync(outdir)) {
  const { rmSync } = await import("fs");
  rmSync(outdir, { recursive: true });
}
mkdirSync(outdir, { recursive: true });

// Build the application
const result = await Bun.build({
  entrypoints: [join(import.meta.dir, "src/main.tsx")],
  outdir,
  minify: true,
  sourcemap: "linked",
  target: "browser",
  splitting: true, // Enable code splitting for better caching
  plugins: [tailwindPlugin],
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
  },
  // External packages that should not be bundled
  external: [],
  naming: {
    // Use content hashes for cache busting
    entry: "[name]-[hash].[ext]",
    chunk: "[name]-[hash].[ext]",
    asset: "[name]-[hash].[ext]",
  },
});

if (!result.success) {
  console.error("Build failed:");
  for (const log of result.logs) {
    console.error(log);
  }
  process.exit(1);
}

// Copy public assets
const publicDir = join(import.meta.dir, "public");
if (existsSync(publicDir)) {
  cpSync(publicDir, outdir, { recursive: true });
  console.log("Copied public assets");
}

// Generate index.html with correct script paths
const indexHtmlTemplate = readFileSync(
  join(import.meta.dir, "index.html"),
  "utf-8"
);

// Find the main entry output
const mainOutput = result.outputs.find(
  (o) => o.kind === "entry-point" && o.path.includes("main")
);
const mainScriptPath = mainOutput
  ? "/" + mainOutput.path.replace(outdir + "/", "")
  : "/main.js";

// Find CSS outputs
const cssOutputs = result.outputs.filter((o) => o.path.endsWith(".css"));
const cssLinks = cssOutputs
  .map((css) => {
    const cssPath = "/" + css.path.replace(outdir + "/", "");
    return `<link rel="stylesheet" href="${cssPath}">`;
  })
  .join("\n    ");

// Transform index.html for production
let indexHtml = indexHtmlTemplate
  // Replace the development script with production bundle
  .replace(
    '<script type="module" src="/src/main.tsx"></script>',
    `${cssLinks ? cssLinks + "\n    " : ""}<script type="module" src="${mainScriptPath}"></script>`
  )
  // Remove Vite-specific references
  .replace('href="/vite.svg"', 'href="/favicon.svg"');

writeFileSync(join(outdir, "index.html"), indexHtml);
console.log("Generated index.html");

// Output build summary
console.log("\nBuild completed successfully!\n");
console.log("Output files:");
for (const output of result.outputs) {
  const relativePath = output.path.replace(outdir + "/", "");
  const size = (output.size / 1024).toFixed(2);
  console.log(`  ${relativePath} (${size} KB)`);
}

console.log(`\nTotal outputs: ${result.outputs.length} files`);
console.log(`Output directory: ${outdir}`);
