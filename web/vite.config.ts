import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` proxies the API to a running shipyard (ui or serve):
//   SHIPYARD_API=http://127.0.0.1:7878 npm run dev
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    proxy: {
      "/api": { target: process.env.SHIPYARD_API ?? "http://127.0.0.1:7878", changeOrigin: false },
    },
  },
});
