const { defineConfig } = require('@vue/cli-service')
const path = require("path");

module.exports = defineConfig({
    devServer: {
      port: 10808,  // 원하는 포트 번호로 변경
      host: "0.0.0.0", // 내부/외부 접속 모두 허용
      allowedHosts: "all", // ✅ Invalid Host header 해결

      historyApiFallback: {
        disableDotRule: true,
        rewrites: [
          // ✅ WebSocket은 그대로
          { from: /^\/sockjs-node.*$/, to: ctx => ctx.parsedUrl.pathname },

          // ✅ /login, /logout은 Vue SPA 엔트리로 리다이렉트하지 않음
          { from: /^\/(login|logout)(\/.*)?$/, to: '/' },

          // ✅ 그 외 모든 SPA 경로는 index.html 로 fallback
          { from: /^\/.*$/, to: '/dashboard/index.html' }
        ]
      }
    },
    transpileDependencies: true,
    pages: {
        // 로그인 페이지 설정: 빌드시 index.html로 생성됨
        login: {
            entry: "src/login/main.js",
            template: "public/login.html",
            filename: "index.html",
            title: "TimeWeaver-UI Login",
        },
        // 대시보드 페이지 설정
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
                "@config": path.resolve(__dirname, "config.js"), // ✅ `@config`로 루트의 `config.js`를 참조
                "@api": path.resolve(__dirname, "api.js"), // ✅ `@api`로 루트의 `api.js`를 참조
            },
        },
    },
});
