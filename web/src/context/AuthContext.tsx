import { createContext, useContext, useState } from "react";
import type { ReactNode } from "react";

type Role = "admin" | "operator" | "viewer";

type AuthContextType = {
    token: string;
    role: Role;
    login: (token: string, role: Role) => void;
    logout: () => void;
};

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
    const [token, setToken] = useState(
        () => sessionStorage.getItem("token") ?? "",
    );
    const [role, setRole] = useState<Role>(
        () => (sessionStorage.getItem("role") as Role) ?? "viewer",
    );

    const login = (newToken: string, newRole: Role) => {
        sessionStorage.setItem("token", newToken);
        sessionStorage.setItem("role", newRole);
        setToken(newToken);
        setRole(newRole);
    };

    const logout = () => {
        sessionStorage.removeItem("token");
        sessionStorage.removeItem("role");
        setToken("");
        setRole("viewer");
    };

    return (
        <AuthContext.Provider value={{ token, role, login, logout }}>
            {children}
        </AuthContext.Provider>
    );
}

export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) throw new Error("useAuth must be used within AuthProvider");
    return ctx;
}
