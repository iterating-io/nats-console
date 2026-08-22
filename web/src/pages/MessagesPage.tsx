import { useEffect, useState, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../context/AuthContext";
import { useApiBase } from "../hooks/useApiBase";
import type { Account } from "../types";

type Stream = { name: string };

export default function MessagesPage() {
    const apiBase = useApiBase();
    const navigate = useNavigate();
    const { token, logout } = useAuth();
    const [accounts, setAccounts] = useState<Account[]>([]);
    const [streams, setStreams] = useState<Stream[]>([]);
    const [selectedAccountPublicKey, setSelectedAccountPublicKey] =
        useState("");
    const [selectedPublishAccountPublicKey, setSelectedPublishAccountPublicKey] =
        useState("");
    const [selectedStream, setSelectedStream] = useState("");
    const [error, setError] = useState("");
    const [message, setMessage] = useState<any>(null);
    const [publishSubject, setPublishSubject] = useState("");
    const [publishMessage, setPublishMessage] = useState("");
    const [publishLoading, setPublishLoading] = useState(false);
    const [publishInfo, setPublishInfo] = useState("");
    const [showPublishAllow, setShowPublishAllow] = useState(false);
    const lastSeqRef = useRef<number>(0);

    const buildStreamsURL = () => {
        if (!selectedAccountPublicKey) return `${apiBase}/api/v1/streams`;
        return `${apiBase}/api/v1/streams?accountPublicKey=${encodeURIComponent(
            selectedAccountPublicKey,
        )}`;
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
        const nextAccounts = data.accounts ?? [];
        setAccounts(nextAccounts);
        setSelectedAccountPublicKey((prev) => {
            if (prev && nextAccounts.some((acc) => acc.publicKey === prev)) {
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
            return firstAvailable?.publicKey ?? "";
        });
        setSelectedPublishAccountPublicKey((prev) => {
            if (prev && nextAccounts.some((acc) => acc.publicKey === prev)) {
                return prev;
            }
            return (
                nextAccounts.find((acc) => !acc.isSystem && acc.jsEnabled)
                    ?.publicKey ?? ""
            );
        });
    };

    const fetchStreams = async () => {
        const res = await fetch(buildStreamsURL(), {
            headers: { Authorization: `Bearer ${token}` },
        });
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
        // Do not auto-select the first stream. Require explicit stream selection
        // so users pick an account first and then a stream.
        setSelectedStream((prev) => {
            if (prev && nextStreams.some((s) => s.name === prev)) return prev;
            return "";
        });
    };

    useEffect(() => {
        fetchAccounts().catch(() => setError("Failed to load accounts."));
    }, []);

    useEffect(() => {
        if (!selectedAccountPublicKey) {
            setStreams([]);
            setSelectedStream("");
            setError("Please select an account to view streams.");
            return;
        }
        const acc = accounts.find(
            (a) => a.publicKey === selectedAccountPublicKey,
        );
        if (!acc?.jsEnabled) {
            setStreams([]);
            setSelectedStream("");
            setError("JetStream is disabled for the selected account.");
            return;
        }
        setError("");
        fetchStreams().catch(() => setError("Failed to load streams."));
    }, [selectedAccountPublicKey, accounts]);

    useEffect(() => {
        if (!selectedAccountPublicKey || !selectedStream) {
            setMessage(null);
            lastSeqRef.current = 0;
            return;
        }
        // If the selected stream does not exist for the current account,
        // don't start polling until streams are refreshed.
        if (!streams.some((s) => s.name === selectedStream)) {
            setMessage(null);
            lastSeqRef.current = 0;
            return;
        }
        // Clear previous message and sequence when starting to poll a newly
        // selected stream so the UI does not retain values from the prior
        // stream while the new stream is being polled.
        lastSeqRef.current = 0;
        setMessage(null);
        setError("");
        let stopped = false;
        let timerId: number | undefined;
        const url = `${apiBase}/api/v1/streams/${encodeURIComponent(selectedStream)}/messages/last?accountPublicKey=${encodeURIComponent(
            selectedAccountPublicKey,
        )}`;

        const decodeBase64ToString = (b64: string) => {
            try {
                const bin = atob(b64);
                const bytes = new Uint8Array(bin.length);
                for (let i = 0; i < bin.length; i++)
                    bytes[i] = bin.charCodeAt(i);
                return new TextDecoder().decode(bytes);
            } catch {
                return atob(b64);
            }
        };

        const poll = async () => {
            const res = await fetch(url, {
                headers: { Authorization: `Bearer ${token}` },
            });
            if (res.status === 401) {
                stopped = true;
                logout();
                navigate("/");
                return;
            }
            if (res.status === 404) {
                // Selected stream does not exist for this account anymore.
                // Stop polling, clear selection and refresh streams so the UI recovers.
                stopped = true;
                setMessage(null);
                setError("");
                setSelectedStream("");
                fetchStreams().catch(() =>
                    setError("Failed to reload streams."),
                );
                return;
            } else if (!res.ok) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                // Stop polling when we get a non-recoverable error (e.g. auth or server error),
                // so we don't repeatedly hammer the server and repeatedly show the error.
                stopped = true;
                setMessage(null);
                setError(data.error ?? "Failed to load message.");
                return;
            }
            const data = await res.json();
            const seq = Number(data.seq ?? 0);
            if (seq === 0) {
                if (!stopped) setMessage(null);
                return;
            }
            if (seq <= lastSeqRef.current) return;
            lastSeqRef.current = seq;
            let decoded = data.payload ?? "";
            if (data.encoding === "base64") {
                decoded = decodeBase64ToString(data.payload ?? "");
            }
            let pretty = decoded;
            try {
                const parsed = JSON.parse(decoded);
                pretty = JSON.stringify(parsed, null, 2);
            } catch {
                // not JSON
            }
            if (!stopped)
                setMessage({
                    seq,
                    subject: data.subject,
                    raw: data.payload,
                    decoded: pretty,
                });
        };

        const tick = async () => {
            try {
                await poll();
            } catch (e) {
                setError(String(e));
            }
            if (!stopped) {
                timerId = window.setTimeout(tick, 1000);
            }
        };

        tick();
        return () => {
            stopped = true;
            if (timerId) clearTimeout(timerId);
        };
    }, [
        selectedAccountPublicKey,
        selectedStream,
        streams,
        apiBase,
        token,
        logout,
        navigate,
    ]);

    const handlePublish = async () => {
        if (!selectedPublishAccountPublicKey || !publishSubject.trim()) return;
        setPublishLoading(true);
        setPublishInfo("");
        setError("");
        try {
            const res = await fetch(
                `${apiBase}/api/v1/messages?accountPublicKey=${encodeURIComponent(selectedPublishAccountPublicKey)}`,
                {
                    method: "POST",
                    headers: {
                        "Content-Type": "application/json",
                        Authorization: `Bearer ${token}`,
                    },
                    body: JSON.stringify({
                        subject: publishSubject,
                        message: publishMessage,
                    }),
                },
            );
            if (!res.ok) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                setError(data.error ?? "Failed to publish message.");
                return;
            }
            const data = (await res.json()) as { sequence: number };
            setPublishInfo(`Message sent (sequence ${data.sequence}).`);
            setPublishMessage("");
        } finally {
            setPublishLoading(false);
        }
    };

    const selectedPublishAccount = accounts.find(
        (account) => account.publicKey === selectedPublishAccountPublicKey,
    );

    return (
        <div className="page-stack">
            <h2>Messages</h2>
            {error && <p className="error">{error}</p>}
            <div className="two-col">
                <section className="panel">
                    <h3>Streams</h3>
                    <div
                        style={{
                            display: "flex",
                            flexDirection: "column",
                            gap: "0.5rem",
                        }}
                    >
                        <label style={{ fontSize: "0.9rem", fontWeight: 600 }}>
                            Account
                        </label>
                        <select
                            aria-label="Select account"
                            value={selectedAccountPublicKey}
                            onChange={(e) => {
                                setSelectedAccountPublicKey(e.target.value);
                                // Clear selected stream when account changes so the user
                                // explicitly chooses a stream for the selected account.
                                setSelectedStream("");
                                setError("");
                            }}
                            className="select-input"
                        >
                            <option value="">Select account…</option>
                            {accounts
                                .filter((acc) => !acc.isSystem)
                                .map((acc) => (
                                    <option
                                        key={acc.publicKey}
                                        value={acc.publicKey}
                                    >
                                        {acc.name} ({acc.operator})
                                        {acc.jsEnabled ? "" : " - JS Disabled"}
                                    </option>
                                ))}
                        </select>

                        <div style={{ marginTop: "0.25rem" }}>
                            <label
                                style={{ fontSize: "0.9rem", fontWeight: 600 }}
                            >
                                Streams
                            </label>
                            <ul
                                className="list"
                                style={{ marginTop: "0.5rem" }}
                            >
                                {streams.length === 0 && (
                                    <li className="muted">No streams found.</li>
                                )}
                                {streams.map((s) => (
                                    <li
                                        key={s.name}
                                        className={`list-row${selectedStream === s.name ? " list-row--active" : ""}`}
                                    >
                                        <button
                                            type="button"
                                            className="list-name-btn"
                                            aria-pressed={
                                                selectedStream === s.name
                                            }
                                            aria-label={`Select stream ${s.name}`}
                                            onClick={() =>
                                                setSelectedStream(s.name)
                                            }
                                        >
                                            {s.name}
                                            {selectedStream === s.name && (
                                                <span className="selected-badge">
                                                    Selected
                                                </span>
                                            )}
                                        </button>
                                    </li>
                                ))}
                            </ul>
                        </div>
                    </div>
                </section>
                <section className="panel">
                    <div className="stack" style={{ marginBottom: "1rem" }}>
                        <h3>Send Message</h3>
                        <label style={{ fontSize: "0.9rem", fontWeight: 600 }}>
                            Sending account
                        </label>
                        <select
                            value={selectedPublishAccountPublicKey}
                            onChange={(e) => {
                                setSelectedPublishAccountPublicKey(e.target.value);
                                setPublishInfo("");
                                setShowPublishAllow(false);
                            }}
                            className="select-input"
                        >
                            <option value="">Select sending account…</option>
                            {accounts
                                .filter((account) => !account.isSystem)
                                .map((account) => (
                                    <option
                                        key={account.publicKey}
                                        value={account.publicKey}
                                    >
                                        {account.name} ({account.operator})
                                        {account.jsEnabled ? "" : " - JS Disabled"}
                                    </option>
                                ))}
                        </select>
                        {selectedPublishAccount && (
                            <div>
                                <button
                                    type="button"
                                    onClick={() => setShowPublishAllow((shown) => !shown)}
                                    aria-expanded={showPublishAllow}
                                    style={{ fontSize: "0.85rem" }}
                                >
                                    {showPublishAllow ? "Hide" : "Show"} Publish Allow
                                </button>
                                {showPublishAllow && (
                                    selectedPublishAccount.publishAllow.length === 0 ? (
                                        <p className="muted">All subjects are allowed.</p>
                                    ) : (
                                        <ul className="list" style={{ marginTop: "0.25rem" }}>
                                            {selectedPublishAccount.publishAllow.map((subject) => (
                                                <li key={subject} className="list-row"><code>{subject}</code></li>
                                            ))}
                                        </ul>
                                    )
                                )}
                            </div>
                        )}
                        <input
                            value={publishSubject}
                            onChange={(e) => setPublishSubject(e.target.value)}
                            placeholder="Subject"
                            disabled={!selectedPublishAccountPublicKey}
                        />
                        <textarea
                            value={publishMessage}
                            onChange={(e) => setPublishMessage(e.target.value)}
                            placeholder="Message"
                            rows={3}
                            disabled={!selectedPublishAccountPublicKey}
                        />
                        <button
                            type="button"
                            disabled={
                                publishLoading ||
                                !selectedPublishAccountPublicKey ||
                                !selectedPublishAccount?.jsEnabled ||
                                !publishSubject.trim()
                            }
                            onClick={handlePublish}
                        >
                            {publishLoading ? "Sending…" : "Send Message"}
                        </button>
                        {publishInfo && <p className="info">{publishInfo}</p>}
                    </div>
                    <h3>Latest Message</h3>
                    {selectedStream && (
                        <div
                            style={{
                                marginTop: "0.5rem",
                                marginBottom: "0.5rem",
                            }}
                        >
                            <span className="selected-badge">
                                {selectedStream}
                            </span>
                        </div>
                    )}
                    {!selectedAccountPublicKey || !selectedStream ? (
                        <p>Select account and stream to begin polling.</p>
                    ) : message ? (
                        <div>
                            <p>
                                <strong>Seq:</strong> {message.seq}
                            </p>
                            <p>
                                <strong>Subject:</strong> {message.subject}
                            </p>
                            <pre className="message-payload">
                                {message.decoded}
                            </pre>
                        </div>
                    ) : (
                        <p>No messages found for this stream.</p>
                    )}
                </section>
            </div>
        </div>
    );
}
