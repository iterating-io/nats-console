import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../context/AuthContext";
import LoginForm from "../components/Auth/LoginForm";
import { useApiBase } from "../hooks/useApiBase";
import "../App.css";

export default function LoginPage() {
    const apiBase = useApiBase();
    const { token } = useAuth();
    const navigate = useNavigate();

    useEffect(() => {
        if (token) {
            navigate("/dashboard", { replace: true });
        }
    }, [token, navigate]);

    return (
        <div className="layout login-layout">
            <header className="topbar">
                <div>
                    <p className="eyebrow">NATS SYSTEM</p>
                    <h1>NATS Console</h1>
                </div>
            </header>
            <main className="login-main">
                <section className="panel login-panel">
                    <h2>Login</h2>
                    <LoginForm apiBase={apiBase} />
                </section>
            </main>
        </div>
    );
}
