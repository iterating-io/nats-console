import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../context/AuthContext";
import { useApiBase } from "../hooks/useApiBase";
import StreamList from "../components/Streams/StreamList";
import StreamForm from "../components/Streams/StreamForm";
import StreamDetail from "../components/Streams/StreamDetail";
import type { Account } from "../types";

type Stream = { name: string };

export default function StreamsPage() {
    const apiBase = useApiBase();
    const navigate = useNavigate();
    const { token, logout } = useAuth();
    const [streams, setStreams] = useState<Stream[]>([]);
    const [accounts, setAccounts] = useState<Account[]>([]);
    const [selectedAccountPublicKey, setSelectedAccountPublicKey] =
        useState("");
    const [selected, setSelected] = useState("");
    const [error, setError] = useState("");
    const [jsEnabled, setJsEnabled] = useState<boolean | null>(null);
    const [jsReason, setJsReason] = useState("");
    const [jsGrantSupported, setJsGrantSupported] = useState(false);
    const [jsGranting, setJsGranting] = useState(false);
    const [jsGrantError, setJsGrantError] = useState("");
    const streamsRequestId = useRef(0);
    const selectedAccountPublicKeyRef = useRef("");

    const selectedAccount = accounts.find(
        (acc) => acc.publicKey === selectedAccountPublicKey,
    );

    const checkJetStreamStatus = async () => {
        const res = await fetch(`${apiBase}/api/v1/system/jetstream-status`, {
            headers: { Authorization: `Bearer ${token}` },
        });
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (res.ok) {
            const data = (await res.json()) as {
                enabled: boolean;
                reason?: string;
                grantSupported?: boolean;
            };
            setJsEnabled(data.enabled);
            setJsReason(data.reason ?? "");
            setJsGrantSupported(data.grantSupported ?? false);
        }
    };

    const handleGrantJetStream = async () => {
        setJsGranting(true);
        setJsGrantError("");
        const res = await fetch(`${apiBase}/api/v1/system/grant-jetstream`, {
            method: "POST",
            headers: { Authorization: `Bearer ${token}` },
        });
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setJsGrantError(data.error ?? "Failed to grant JetStream access.");
            setJsGranting(false);
            return;
        }
        await checkJetStreamStatus();
        setJsGranting(false);
    };

    const buildStreamsURL = (accountPublicKey: string) => {
        return `${apiBase}/api/v1/streams?accountPublicKey=${encodeURIComponent(accountPublicKey)}`;
    };

    const fetchAccounts = async () => {
        const res = await fetch(`${apiBase}/api/v1/accounts`, {
            headers: { Authorization: `Bearer ${token}` },
        });
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            setError("Failed to load accounts.");
            return;
        }
        const data = (await res.json()) as { accounts: Account[] };
        const nextAccounts = (data.accounts ?? []).filter(
            (account) => account.name !== "asyncapi-checker",
        );
        setAccounts(nextAccounts);
        setSelectedAccountPublicKey((prev) => {
            if (prev && nextAccounts.some((acc) => acc.publicKey === prev)) {
                selectedAccountPublicKeyRef.current = prev;
                return prev;
            }
            const firstAvailable =
                nextAccounts.find(
                    (acc) =>
                        !acc.isSystem &&
                        acc.jsEnabled &&
                        (acc.name.toLowerCase().includes("console") ||
                            acc.name.toLowerCase().includes("js")),
                ) ??
                nextAccounts.find((acc) => !acc.isSystem && acc.jsEnabled) ??
                nextAccounts.find((acc) => !acc.isSystem);
            const next = firstAvailable?.publicKey ?? "";
            selectedAccountPublicKeyRef.current = next;
            return next;
        });
    };

    const fetchStreams = async (accountPublicKey = selectedAccountPublicKey) => {
        if (!accountPublicKey) {
            setStreams([]);
            setSelected("");
            return;
        }
        const requestId = ++streamsRequestId.current;
        const res = await fetch(buildStreamsURL(accountPublicKey), {
            headers: { Authorization: `Bearer ${token}` },
        });
        if (
            requestId !== streamsRequestId.current ||
            accountPublicKey !== selectedAccountPublicKeyRef.current
        ) {
            return;
        }
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to load streams.");
            return;
        }
        const data = (await res.json()) as { streams: string[] };
        const nextStreams = data.streams.map((name) => ({ name }));
        setStreams(nextStreams);
        setSelected((prev) => {
            if (prev && nextStreams.some((s) => s.name === prev)) {
                return prev;
            }
            return nextStreams[0]?.name ?? "";
        });
    };

    useEffect(() => {
        checkJetStreamStatus().catch(() => {});
        fetchAccounts().catch(() => setError("Failed to load accounts."));
    }, []);

    useEffect(() => {
        if (!selectedAccountPublicKey) {
            streamsRequestId.current += 1;
            setStreams([]);
            setSelected("");
            setError("Please select an account to view streams.");
            return;
        }
        if (!selectedAccount?.jsEnabled) {
            streamsRequestId.current += 1;
            setStreams([]);
            setSelected("");
            setError("JetStream is disabled for the selected account.");
            return;
        }
        setError("");
        fetchStreams().catch(() => setError("Failed to load streams."));
    }, [selectedAccountPublicKey, selectedAccount?.jsEnabled]);

    const handleCreated = async (name: string, subjects: string[]) => {
        const accountPublicKey = selectedAccountPublicKey;
        if (!accountPublicKey) return;
        setError("");
        const res = await fetch(buildStreamsURL(accountPublicKey), {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                Authorization: `Bearer ${token}`,
            },
            body: JSON.stringify({ name, subjects }),
        });
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to create stream.");
            return;
        }
        if (selectedAccountPublicKeyRef.current === accountPublicKey) {
            await fetchStreams(accountPublicKey);
        }
    };

    const handleDelete = async (name: string) => {
        setError("");
        const basePath = `${apiBase}/api/v1/streams/${encodeURIComponent(name)}`;
        const streamURL = selectedAccountPublicKey
            ? `${basePath}?accountPublicKey=${encodeURIComponent(selectedAccountPublicKey)}`
            : basePath;
        const res = await fetch(streamURL, {
            method: "DELETE",
            headers: { Authorization: `Bearer ${token}` },
        });
        if (!res.ok && res.status !== 204) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to delete stream.");
            return;
        }
        if (selected === name) setSelected("");
        await fetchStreams();
    };

    const handleAuthError = () => {
        logout();
        navigate("/");
    };

    return (
        <div className="page-stack">
            <h2>
                Streams
                <button
                    type="button"
                    style={{ marginLeft: "0.75rem", fontSize: "0.75rem" }}
                    onClick={() =>
                        fetchStreams().catch(() =>
                            setError("Failed to reload streams."),
                        )
                    }
                >
                    Refresh
                </button>
            </h2>

            {jsEnabled === false && (
                <div className="panel jetstream-notice">
                    <div className="jetstream-notice-header">
                        <span className="badge warn">JetStream Disabled</span>
                        <span className="jetstream-notice-title">
                            The system account does not have JetStream access.
                            Streams and Consumers are unavailable.
                        </span>
                    </div>
                    {jsReason && <p className="notice">{jsReason}</p>}
                    {jsGrantError && <p className="error">{jsGrantError}</p>}
                    <button
                        type="button"
                        onClick={handleGrantJetStream}
                        disabled={jsGranting || !jsGrantSupported}
                    >
                        {jsGranting
                            ? "Granting…"
                            : jsGrantSupported
                              ? "Grant JetStream Access"
                              : "Grant Unsupported for SYS Account"}
                    </button>
                </div>
            )}

            {error && <p className="error">{error}</p>}
            <div className="two-col">
                <section className="panel">
                    <StreamForm
                        accounts={accounts}
                        selectedAccountPublicKey={selectedAccountPublicKey}
                        onSelectAccount={(next) => {
                            streamsRequestId.current += 1;
                            selectedAccountPublicKeyRef.current = next;
                            setSelectedAccountPublicKey(next);
                            setSelected("");
                            setError("");
                        }}
                        onCreated={handleCreated}
                    />
                    <StreamList
                        streams={streams}
                        selected={selected}
                        onSelect={setSelected}
                        onDelete={handleDelete}
                    />
                </section>
                {selected && (
                    <StreamDetail
                        streamName={selected}
                        token={token}
                        apiBase={apiBase}
                        onAuthError={handleAuthError}
                        accountPublicKey={selectedAccountPublicKey}
                        onNotFound={() => {
                            setSelected("");
                            fetchStreams().catch(() => {});
                        }}
                    />
                )}
            </div>
        </div>
    );
}
