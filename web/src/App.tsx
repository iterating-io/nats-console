import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { AuthProvider } from "./context/AuthContext";
import LoginPage from "./pages/LoginPage";
import DashboardLayout from "./pages/DashboardLayout";
import AccountsPage from "./pages/AccountsPage";
import StreamsPage from "./pages/StreamsPage";

function App() {
    const baseUrl = import.meta.env.BASE_URL ?? "/";
    const routerBase =
        baseUrl !== "/" && baseUrl.endsWith("/")
            ? baseUrl.slice(0, -1)
            : baseUrl;

    return (
        <AuthProvider>
            <BrowserRouter basename={routerBase}>
                <Routes>
                    <Route path="/" element={<LoginPage />} />
                    <Route path="/dashboard" element={<DashboardLayout />}>
                        <Route
                            index
                            element={<Navigate to="accounts" replace />}
                        />
                        <Route path="accounts" element={<AccountsPage />} />
                        <Route path="streams" element={<StreamsPage />} />
                    </Route>
                    <Route path="*" element={<Navigate to="/" replace />} />
                </Routes>
            </BrowserRouter>
        </AuthProvider>
    );
}

export default App;
