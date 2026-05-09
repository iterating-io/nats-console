import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

function normalizeBasePath(raw: string | undefined): string {
    if (!raw) return "/";

    const trimmed = raw.trim();
    if (trimmed === "" || trimmed === "/") return "/";

    const withLeadingSlash = trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
    return withLeadingSlash.endsWith("/")
        ? withLeadingSlash
        : `${withLeadingSlash}/`;
}

function escapeForRegex(input: string): string {
    return input.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// https://vite.dev/config/
export default defineConfig(() => {
    const base = normalizeBasePath(process.env.WEB_BASE_PATH);
    const baseNoTrailingSlash = base === "/" ? "" : base.slice(0, -1);
    const apiPrefix = `${baseNoTrailingSlash}/api`;

    return {
        base,
        plugins: [react()],
        server: {
            proxy: {
                [apiPrefix]: {
                    target: "http://localhost:8080",
                    changeOrigin: true,
                    rewrite: (path) => {
                        if (baseNoTrailingSlash === "") {
                            return path;
                        }
                        return path.replace(
                            new RegExp(
                                `^${escapeForRegex(baseNoTrailingSlash)}`,
                            ),
                            "",
                        );
                    },
                },
            },
        },
    };
});
