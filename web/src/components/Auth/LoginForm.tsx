import { useState } from "react";
import type { SubmitEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../../context/AuthContext";

type LoginResponse = {
    accessToken: string;
    role: "admin" | "operator" | "viewer";
};

type Props = {
    apiBase: string;
};

export default function LoginForm({ apiBase }: Props) {
    const { login } = useAuth();
    const navigate = useNavigate();

    const [username, setUsername] = useState("admin");
    const [password, setPassword] = useState("admin");
    const [error, setError] = useState("");

    const onLogin = async (event: SubmitEvent<HTMLFormElement>) => {
        event.preventDefault();
        setError("");

        const res = await fetch(`${apiBase}/api/auth/login`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ username, password }),
        });

        if (!res.ok) {
            setError("Login failed: please check your credentials.");
            return;
        }

        const data = (await res.json()) as LoginResponse;
        login(data.accessToken, data.role);
        navigate("/dashboard");
    };

    return (
        <form onSubmit={onLogin} className="stack">
            <input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="username"
                autoComplete="username"
            />
            <input
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="password"
                type="password"
                autoComplete="current-password"
            />
            <button type="submit">Sign in</button>
            <p className="muted">
                sample: admin/admin, operator/operator, viewer/viewer
            </p>
            {error && <p className="error">{error}</p>}
        </form>
    );
}
