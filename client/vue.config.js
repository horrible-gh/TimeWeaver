const { defineConfig } = require('@vue/cli-service')
const path = require("path");
const fs = require("fs");

// client/config.js is a local-only file (.gitignore'd, written by
// scripts/setup-windows.ps1 / setup-linux.sh) and is never committed. Any
// checkout that hasn't run that installer - CI, preview builders, a fresh
// `git clone` - won't have it, and webpack would fail to resolve "@config"
// with "Module not found". Fall back to the committed config.sample.js
// (same env-derived default) so the app always compiles out of the box;
// a real config.js, when present, still takes priority.
const localConfigPath = path.resolve(__dirname, "config.js");
const configPath = fs.existsSync(localConfigPath)
    ? localConfigPath
    : path.resolve(__dirname, "config.sample.js");

module.exports = defineConfig({
    devServer: {
      port: 10808,  // Change to the desired port number
      host: "0.0.0.0", // Allow internal and external access
      allowedHosts: "all", // ✅ Resolve Invalid Host header

      // ✅ HMR / live-reload socket (webpack-dev-server v4, default path "/ws").
      // Derive the socket host/port from the browser's own location so HMR
      // connects back to whatever origin the page was opened at — localhost OR a
      // LAN IP (e.g. http://192.168.0.251:10808). The sentinels hostname
      // "0.0.0.0" and port 0 tell the client to read window.location at runtime,
      // preventing the "ws://localhost:10808/ws ... ERR_CONNECTION_REFUSED" noise
      // when the page is served over the LAN rather than localhost.
      client: {
        webSocketURL: {
          hostname: "0.0.0.0",
          port: 0,
          pathname: "/ws",
        },
      },

      historyApiFallback: {
        disableDotRule: true,
        rewrites: [
          // ✅ Keep the HMR WebSocket paths as-is (do not fall back to index.html).
          //    "/ws" = webpack-dev-server v4 socket; "/sockjs-node" = legacy v3.
          { from: /^\/ws(\/.*)?$/, to: ctx => ctx.parsedUrl.pathname },
          { from: /^\/sockjs-node.*$/, to: ctx => ctx.parsedUrl.pathname },

          // ✅ Do not redirect /login or /logout to the Vue SPA entry
          { from: /^\/(login|logout)(\/.*)?$/, to: '/' },

          // ✅ Fallback all other SPA paths to index.html
          { from: /^\/.*$/, to: '/dashboard/index.html' }
        ]
      }
    },
    transpileDependencies: true,
    pages: {
        // Login page setting: generated as index.html at build time
        login: {
            entry: "src/login/main.js",
            template: "public/login.html",
            filename: "index.html",
            title: "TimeWeaver-UI Login",
        },
        // Dashboard page setting
        dashboard: {
            entry: "src/dashboard/main.js",
            template: "public/dashboard.html",
            filename: "dashboard/index.html",
            title: "TimeWeaver-UI Dashboard",
        },
    },
    configureWebpack: {
        resolve: {
            alias: {
                "@config": configPath, // ✅ Reference config.js (or config.sample.js fallback) as @config
                "@api": path.resolve(__dirname, "api.js"), // ✅ Reference root api.js as @api
            },
        },
    },
});
