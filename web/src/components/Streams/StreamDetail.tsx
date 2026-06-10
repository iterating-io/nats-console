import { useEffect, useState } from "react";
import ConsumerList from "../Consumers/ConsumerList";
import ConsumerForm from "../Consumers/ConsumerForm";

type Consumer = { name: string; filterSubject: string };

type StreamDetailType = {
    name: string;
    subjects: string[];
    config?: any;
    state?: any;
    cluster?: any;
    created?: string;
};

type Props = {
    streamName: string;
    token: string;
    apiBase: string;
    accountPublicKey?: string;
    onAuthError: () => void;
    onNotFound?: () => void;
};

export default function StreamDetail({
    streamName,
    token,
    apiBase,
    accountPublicKey,
    onAuthError,
    onNotFound,
}: Props) {
    const [stream, setStream] = useState<StreamDetailType | null>(null);
    const [consumers, setConsumers] = useState<Consumer[]>([]);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(false);
    const [newSubject, setNewSubject] = useState("");
    const [subjectLoading, setSubjectLoading] = useState(false);
    const [showConfig, setShowConfig] = useState(false);
    const [showState, setShowState] = useState(false);

    const withAccountScope = (path: string) => {
        if (!accountPublicKey) {
            return path;
        }
        const separator = path.includes("?") ? "&" : "?";
        return `${path}${separator}accountPublicKey=${encodeURIComponent(accountPublicKey)}`;
    };

    const fetchConsumers = async (name: string) => {
        const consumersRes = await fetch(
            withAccountScope(
                `${apiBase}/api/v1/streams/${encodeURIComponent(name)}/consumers`,
            ),
            { headers: { Authorization: `Bearer ${token}` } },
        );
        if (consumersRes.status === 401) {
            onAuthError();
            return;
        }
        if (!consumersRes.ok) {
            setError("Failed to load consumers.");
            return;
        }
        const consumersData = (await consumersRes.json()) as {
            consumers: Consumer[];
        };
        setConsumers(consumersData.consumers || []);
    };

    useEffect(() => {
        const fetchData = async () => {
            setLoading(true);
            setError("");
            try {
                const streamRes = await fetch(
                    withAccountScope(
                        `${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}`,
                    ),
                    {
                        headers: { Authorization: `Bearer ${token}` },
                    },
                );
                if (streamRes.status === 401) {
                    onAuthError();
                    return;
                }
                if (streamRes.status === 404) {
                    onNotFound?.();
                    return;
                }
                if (!streamRes.ok) {
                    setError("Failed to load stream details.");
                    return;
                }
                const streamData = (await streamRes.json()) as StreamDetailType;
                setStream({
                    ...streamData,
                    subjects: streamData.subjects ?? [],
                });
                await fetchConsumers(streamName);
            } catch {
                setError("Failed to load stream data.");
            } finally {
                setLoading(false);
            }
        };

        if (streamName) {
            fetchData();
        }
    }, [streamName, token, apiBase, onAuthError, accountPublicKey]);

    const handleConsumerCreated = async (
        streamValue: string,
        name: string,
        filterSubject: string,
    ) => {
        setError("");
        const res = await fetch(
            withAccountScope(
                `${apiBase}/api/v1/streams/${encodeURIComponent(streamValue)}/consumers`,
            ),
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ name, filterSubject }),
            },
        );
        if (res.status === 401) {
            onAuthError();
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to create consumer.");
            return;
        }
        await fetchConsumers(streamValue);
    };

    const handleConsumerDelete = async (name: string) => {
        setError("");
        const res = await fetch(
            withAccountScope(
                `${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}/consumers/${encodeURIComponent(name)}`,
            ),
            { method: "DELETE", headers: { Authorization: `Bearer ${token}` } },
        );
        if (res.status === 401) {
            onAuthError();
            return;
        }
        if (!res.ok && res.status !== 204) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to delete consumer.");
            return;
        }
        await fetchConsumers(streamName);
    };

    const patchSubjects = async (subjects: string[]) => {
        setSubjectLoading(true);
        setError("");
        try {
            const res = await fetch(
                withAccountScope(
                    `${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}`,
                ),
                {
                    method: "PATCH",
                    headers: {
                        "Content-Type": "application/json",
                        Authorization: `Bearer ${token}`,
                    },
                    body: JSON.stringify({ subjects }),
                },
            );
            if (res.status === 401) {
                onAuthError();
                return;
            }
            if (!res.ok) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                setError(data.error ?? "Failed to update subjects.");
                return;
            }
            const data = (await res.json()) as { subjects: string[] | null };
            setStream((prev) =>
                prev ? { ...prev, subjects: data.subjects ?? subjects } : prev,
            );
        } finally {
            setSubjectLoading(false);
        }
    };

    const handleAddSubject = async () => {
        const subject = newSubject.trim();
        if (!subject || !stream) return;
        if (stream.subjects.includes(subject)) return;
        await patchSubjects([...stream.subjects, subject]);
        setNewSubject("");
    };

    const handleRemoveSubject = async (subject: string) => {
        if (!stream) return;
        await patchSubjects(stream.subjects.filter((s) => s !== subject));
    };

    if (loading) {
        return <div className="panel">Loading stream details...</div>;
    }

    if (!stream) {
        return <div className="panel error">Stream not found.</div>;
    }

    const sortedSubjects = [...stream.subjects].sort();

    return (
        <section className="panel">
            <h3>{stream.name}</h3>
            <div className="stack">
                <div>
                    <strong>Subjects:</strong>
                    {sortedSubjects.length === 0 && (
                        <span
                            className="muted"
                            style={{ marginLeft: "0.5rem", fontSize: "0.85em" }}
                        >
                            none
                        </span>
                    )}
                    <ul className="list">
                        {sortedSubjects.map((s) => (
                            <li key={s} className="list-row">
                                <code>{s}</code>
                                <button
                                    type="button"
                                    className="delete-btn"
                                    disabled={subjectLoading}
                                    onClick={() => {
                                        if (
                                            !window.confirm(
                                                `Remove subject '${s}' from stream '${streamName}'?`,
                                            )
                                        )
                                            return;
                                        handleRemoveSubject(s);
                                    }}
                                >
                                    ✕
                                </button>
                            </li>
                        ))}
                    </ul>
                    <div
                        style={{
                            display: "flex",
                            gap: "0.4rem",
                            marginTop: "0.4rem",
                        }}
                    >
                        <input
                            value={newSubject}
                            onChange={(e) => setNewSubject(e.target.value)}
                            placeholder="new subject (e.g. orders.>)"
                            disabled={subjectLoading}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                    e.preventDefault();
                                    handleAddSubject();
                                }
                            }}
                        />
                        <button
                            type="button"
                            disabled={subjectLoading || !newSubject.trim()}
                            onClick={handleAddSubject}
                        >
                            Add
                        </button>
                    </div>
                </div>

                {(stream.config || stream.state) && (
                    <div style={{ marginTop: "0.5rem" }}>
                        {stream.config && (
                            <div>
                                <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
                                    <strong>Stream Config</strong>
                                    <button
                                        type="button"
                                        onClick={() => setShowConfig((s) => !s)}
                                        aria-expanded={showConfig}
                                        style={{ fontSize: "0.85rem" }}
                                    >
                                        {showConfig ? "Hide" : "Show"}
                                    </button>
                                </div>
                                {showConfig && (
                                    <pre style={{ whiteSpace: "pre-wrap", marginTop: "0.25rem" }}>
                                        {JSON.stringify(stream.config, null, 2)}
                                    </pre>
                                )}
                            </div>
                        )}

                        {stream.state && (
                            <div style={{ marginTop: "0.5rem" }}>
                                <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
                                    <strong>Stream State</strong>
                                    <button
                                        type="button"
                                        onClick={() => setShowState((s) => !s)}
                                        aria-expanded={showState}
                                        style={{ fontSize: "0.85rem" }}
                                    >
                                        {showState ? "Hide" : "Show"}
                                    </button>
                                </div>
                                {showState && (
                                    <pre style={{ whiteSpace: "pre-wrap", marginTop: "0.25rem" }}>
                                        {JSON.stringify(stream.state, null, 2)}
                                    </pre>
                                )}
                            </div>
                        )}
                    </div>
                )}

                {error && <p className="error">{error}</p>}

                <div className="stack">
                    <ConsumerForm
                        streams={[streamName]}
                        onCreated={handleConsumerCreated}
                        fixedStream={streamName}
                    />
                    <ConsumerList
                        consumers={consumers}
                        onDelete={handleConsumerDelete}
                    />
                </div>
            </div>
        </section>
    );
}
