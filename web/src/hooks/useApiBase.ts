import { useMemo } from "react";

export function useApiBase(): string {
    return useMemo(() => import.meta.env.VITE_API_BASE ?? "", []);
}
