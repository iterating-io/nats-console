import { useMemo } from "react";

export function useApiBase(): string {
    return useMemo(() => {
        const baseUrl = import.meta.env.BASE_URL ?? "/";
        if (baseUrl === "/") {
            return "";
        }

        return baseUrl.endsWith("/") ? baseUrl.slice(0, -1) : baseUrl;
    }, []);
}
