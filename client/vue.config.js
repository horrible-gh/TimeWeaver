const { defineConfig } = require('@vue/cli-service')
const path = require("path");

module.exports = defineConfig({
    devServer: {
      port: 10808,  // Change to the desired port number
      host: "0.0.0.0", // Allow internal and external access
      allowedHosts: "all", // ✅ Resolve Invalid Host header

      historyApiFallback: {
        disableDotRule: true,
        rewrites: [
          // ✅ Keep WebSocket as-is
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
                "@config": path.resolve(__dirname, "config.js"), // ✅ Reference root config.js as @config
                "@api": path.resolve(__dirname, "api.js"), // ✅ Reference root api.js as @api
            },
        },
    },
});
