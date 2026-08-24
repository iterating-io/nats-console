import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useApiBase } from "../hooks/useApiBase";
import { useAuth } from "../context/AuthContext";
import type { Account } from "../types";

type ImportItem = { account: Account; imported: boolean; alias: string };
type AsyncAPIState = { checkerExists: boolean; checker?: Account; imports: ImportItem[] };

export default function AsyncAPIPage() {
    const apiBase = useApiBase();
    const navigate = useNavigate();
    const { token, logout } = useAuth();
    const [state, setState] = useState<AsyncAPIState | null>(null);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState("");
    const [creds, setCreds] = useState("");

    const headers = { Authorization: `Bearer ${token}` };
    const handleUnauthorized = () => { logout(); navigate("/"); };
    const load = async () => {
        const res = await fetch(`${apiBase}/api/v1/asyncapi`, { headers });
        if (res.status === 401) return handleUnauthorized();
        if (!res.ok) { setError("Failed to load AsyncAPI configuration."); return; }
        setState((await res.json()) as AsyncAPIState);
    };
    useEffect(() => { load().catch(() => setError("Failed to load AsyncAPI configuration.")); }, []);

    const createChecker = async () => {
        setLoading("create"); setError("");
        try {
            const res = await fetch(`${apiBase}/api/v1/asyncapi`, { method: "POST", headers });
            if (res.status === 401) return handleUnauthorized();
            if (!res.ok) { const data = await res.json().catch(() => ({})) as { error?: string }; setError(data.error ?? "Failed to create checker account."); return; }
            await load();
        } finally { setLoading(""); }
    };
    const toggleImport = async (item: ImportItem) => {
        setLoading(item.account.publicKey); setError("");
        try {
            const res = await fetch(`${apiBase}/api/v1/asyncapi/imports/${encodeURIComponent(item.account.publicKey)}`, {
                method: "POST", headers: { ...headers, "Content-Type": "application/json" }, body: JSON.stringify({ enabled: !item.imported }),
            });
            if (res.status === 401) return handleUnauthorized();
            if (!res.ok) { const data = await res.json().catch(() => ({})) as { error?: string }; setError(data.error ?? "Failed to update import."); return; }
            await load(); setCreds("");
        } finally { setLoading(""); }
    };
    const showCreds = async () => {
        if (!state?.checker) return;
        setLoading("creds"); setError("");
        try {
            const res = await fetch(`${apiBase}/api/v1/accounts/${encodeURIComponent(state.checker.operator)}/${encodeURIComponent(state.checker.publicKey)}/users/asyncapi-checker/creds`, { headers });
            if (res.status === 401) return handleUnauthorized();
            if (!res.ok) { const data = await res.json().catch(() => ({})) as { error?: string }; setError(data.error ?? "Failed to export checker credentials."); return; }
            setCreds(((await res.json()) as { creds: string }).creds);
        } finally { setLoading(""); }
    };
    const downloadCreds = () => {
        const url = URL.createObjectURL(new Blob([creds], { type: "text/plain;charset=utf-8" }));
        const link = document.createElement("a"); link.href = url; link.download = "asyncapi-checker.creds"; link.click(); URL.revokeObjectURL(url);
    };

    return <div className="page">
        <div className="page-header"><div><h2>AsyncAPI</h2><p className="muted">Centralized, read-only JetStream Stream Info access across service accounts.</p></div></div>
        {error && <p className="error">{error}</p>}
        {!state ? <p className="muted">Loading AsyncAPI configuration…</p> : !state.checkerExists ? (
            <section className="panel"><h3>Create the checker account</h3><p className="muted">Create the single <code>asyncapi-checker</code> account. It receives no access until you import a service account API below.</p><button type="button" disabled={loading === "create"} onClick={createChecker}>{loading === "create" ? "Creating…" : "Create AsyncAPI Checker"}</button></section>
        ) : <>
            <section className="panel"><h3>Checker account</h3><p><code>{state.checker?.name}</code></p><p className="muted">The checker can publish only to enabled aliases and receive replies on <code>_INBOX.&gt;</code>.</p><button type="button" disabled={loading === "creds"} onClick={showCreds}>{loading === "creds" ? "Loading…" : "Show checker credentials"}</button></section>
            {creds && <section className="panel"><h3>Checker credentials</h3><textarea readOnly value={creds} rows={12} style={{ width: "100%" }} /><div style={{ marginTop: "0.6rem", display: "flex", gap: "0.5rem" }}><button type="button" onClick={() => navigator.clipboard.writeText(creds)}>Copy</button><button type="button" onClick={downloadCreds}>Download .creds</button></div><p className="muted">Export fresh credentials after changing imports, then reconnect the checker script.</p></section>}
            <section className="panel"><h3>Service account APIs</h3><p className="muted">Enable an import to expose only that account’s Stream Info API through its checker alias.</p><ul className="list">{state.imports.length === 0 && <li className="muted">No service accounts are available.</li>}{state.imports.map((item) => <li key={item.account.publicKey} className="list-row"><div><strong>{item.account.name}</strong>{item.imported ? <span className="badge ok" style={{ marginLeft: "0.5rem" }}>Imported</span> : <span className="badge" style={{ marginLeft: "0.5rem" }}>Not imported</span>}<div className="muted"><code>{item.alias}</code></div>{!item.account.jsEnabled && <div className="muted">JetStream is disabled</div>}</div><button type="button" disabled={loading === item.account.publicKey || (!item.imported && !item.account.jsEnabled)} onClick={() => toggleImport(item)}>{loading === item.account.publicKey ? "Updating…" : item.imported ? "Remove Import" : "Import API"}</button></li>)}</ul></section>
        </>}
    </div>;
}
