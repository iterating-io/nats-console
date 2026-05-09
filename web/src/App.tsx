import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { AuthProvider } from "./context/AuthContext";
import LoginPage from "./pages/LoginPage";
import DashboardLayout from "./pages/DashboardLayout";
import AccountsPage from "./pages/AccountsPage";
import StreamsPage from "./pages/StreamsPage";

function App() {
    return (
        <AuthProvider>
            <BrowserRouter basename={import.meta.env.BASE_URL}>
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
