import { useEffect } from "react";
import { Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../context/AuthContext";
import Sidebar from "../components/Sidebar/Sidebar";
import "../App.css";

export default function DashboardLayout() {
    const { token, logout } = useAuth();
    const navigate = useNavigate();

    useEffect(() => {
        if (!token) {
            navigate("/", { replace: true });
        }
    }, [token, navigate]);

    const onLogout = () => {
        logout();
        navigate("/");
    };

    return (
        <div className="app-shell">
            <Sidebar />
            <div className="main-area">
                <header className="topbar">
                    <div />
                    <div className="health">
                        <button
                            type="button"
                            className="logout-btn"
                            onClick={onLogout}
                        >
                            Logout
                        </button>
                    </div>
                </header>
                <main className="content">
                    <Outlet />
                </main>
            </div>
        </div>
    );
}
