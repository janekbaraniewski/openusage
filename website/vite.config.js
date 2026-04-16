import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/",
  plugins: [
    react(),
    {
      name: "redirect-legacy-openusage-base",
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          const url = req.url || "/";
          if (!url.startsWith("/openusage")) {
            next();
            return;
          }

          const redirectTo = url.replace(/^\/openusage/, "") || "/";
          res.statusCode = 302;
          res.setHeader("Location", redirectTo);
          res.end();
        });
      },
    },
  ],
});
