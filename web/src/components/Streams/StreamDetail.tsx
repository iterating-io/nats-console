import { useEffect, useState } from "react";
import ConsumerList from "../Consumers/ConsumerList";
import ConsumerForm from "../Consumers/ConsumerForm";
import type { Account } from "../../types";

type Consumer = { name: string; filterSubject: string };

type StreamDetailType = {
    name: string;
    subjects: string[];
    config?: any;
    state?: any;
    cluster?: any;
    created?: string;
};

type SourceStream = {
    name: string;
    accountPublicKey: string;
    filterSubjects: string[];
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
    const [sourceAccounts, setSourceAccounts] = useState<Account[]>([]);
    const [sourceAccountPublicKey, setSourceAccountPublicKey] = useState("");
    const [sourceStreams, setSourceStreams] = useState<string[]>([]);
    const [sourceName, setSourceName] = useState("");
    const [sourceFilterInput, setSourceFilterInput] = useState("");
    const [sourceFilters, setSourceFilters] = useState<string[]>([]);
    const [sourceCardFilterInputs, setSourceCardFilterInputs] = useState<Record<string, string>>({});
    const [sourceLoading, setSourceLoading] = useState(false);

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

    useEffect(() => {
        if (!accountPublicKey) return;
        fetch(`${apiBase}/api/v1/accounts/source-accounts?accountPublicKey=${encodeURIComponent(accountPublicKey)}`, { headers: { Authorization: `Bearer ${token}` } })
            .then(async (res) => res.ok ? (res.json() as Promise<{ accounts: Account[] }>) : { accounts: [] })
            .then((data) => setSourceAccounts(data.accounts ?? []))
            .catch(() => setSourceAccounts([]));
    }, [accountPublicKey, apiBase, token]);

    useEffect(() => {
        if (!sourceAccountPublicKey) { setSourceStreams([]); setSourceName(""); return; }
        fetch(`${apiBase}/api/v1/streams?accountPublicKey=${encodeURIComponent(sourceAccountPublicKey)}`, { headers: { Authorization: `Bearer ${token}` } })
            .then(async (res) => res.ok ? (res.json() as Promise<{ streams: string[] }>) : { streams: [] })
            .then((data) => { setSourceStreams(data.streams ?? []); setSourceName(""); })
            .catch(() => setSourceStreams([]));
    }, [sourceAccountPublicKey, apiBase, token]);

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
    const rawSources: unknown = stream.config?.sources;
    const configuredSources = Array.isArray(rawSources)
        ? rawSources
              .flatMap((source): { name: string; accountPublicKey: string; filterSubject: string }[] => {
                  if (typeof source !== "object" || source === null) return [];
                  const { name, filter_subject: filterSubject, external } = source as {
                      name?: unknown;
                      filter_subject?: unknown;
                      external?: { api?: unknown };
                  };
                  if (typeof name !== "string") return [];
                  const apiPrefix = typeof external?.api === "string" ? external.api : "";
                  const sourceAccountMatch = /^\$JS\.SOURCE\.(.+)\.API$/.exec(apiPrefix);
                  return [{
                      name,
                      accountPublicKey: sourceAccountMatch?.[1] ?? accountPublicKey ?? "",
                      filterSubject: typeof filterSubject === "string" ? filterSubject : "",
                  }];
              })
              .sort((a, b) => a.name.localeCompare(b.name))
        : [];
    const sources = Array.from(
        configuredSources.reduce((grouped, source) => {
            const key = `${source.accountPublicKey}:${source.name}`;
            const existing = grouped.get(key) ?? { name: source.name, accountPublicKey: source.accountPublicKey, filterSubjects: [] };
            existing.filterSubjects.push(source.filterSubject);
            grouped.set(key, existing);
            return grouped;
        }, new Map<string, SourceStream>()).values(),
    );
    const sourceKey = (source: SourceStream) => `${source.accountPublicKey}:${source.name}`;

    const handlePurge = async () => {
        if (!stream) return;
        if (!window.confirm(`Purge all messages from stream '${streamName}'? This cannot be undone.`)) return;
        setError("");
        setLoading(true);
        try {
            const res = await fetch(
                withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}/purge`),
                { method: "POST", headers: { Authorization: `Bearer ${token}` } },
            );
            if (res.status === 401) {
                onAuthError();
                return;
            }
            if (!res.ok) {
                const data = (await res.json().catch(() => ({}))) as { error?: string };
                setError(data.error ?? "Failed to purge stream.");
                return;
            }
            // reload stream details and consumers
            const streamRes = await fetch(
                withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}`),
                { headers: { Authorization: `Bearer ${token}` } },
            );
            if (streamRes.status === 401) {
                onAuthError();
                return;
            }
            if (streamRes.ok) {
                const streamData = (await streamRes.json()) as StreamDetailType;
                setStream({ ...streamData, subjects: streamData.subjects ?? [] });
            } else {
                setError("Failed to reload stream after purge.");
            }
            await fetchConsumers(streamName);
        } catch {
            setError("Failed to purge stream.");
        } finally {
            setLoading(false);
        }
    };

    const handleAddSource = async () => {
        if (!sourceAccountPublicKey || !sourceName) return;
        setSourceLoading(true); setError("");
        try {
            const res = await fetch(withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}/sources`), {
                method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
                body: JSON.stringify({ sourceAccountPublicKey, sourceName, filterSubject: sourceFilters.join(",") }),
            });
            if (res.status === 401) { onAuthError(); return; }
            if (!res.ok) { const data = (await res.json().catch(() => ({}))) as { error?: string }; setError(data.error ?? "Failed to add stream source."); return; }
            const streamRes = await fetch(withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}`), { headers: { Authorization: `Bearer ${token}` } });
            if (streamRes.ok) { const data = (await streamRes.json()) as StreamDetailType; setStream({ ...data, subjects: data.subjects ?? [] }); }
            setSourceFilterInput("");
            setSourceFilters([]);
        } finally { setSourceLoading(false); }
    };

    const handleAddSourceFilter = () => {
        const filter = sourceFilterInput.trim();
        if (!filter || sourceFilters.includes(filter)) return;
        setSourceFilters((prev) => [...prev, filter]);
        setSourceFilterInput("");
    };

    const handleAddFilterToSource = async (source: SourceStream) => {
        const key = sourceKey(source);
        const filter = (sourceCardFilterInputs[key] ?? "").trim();
        if (!filter || source.filterSubjects.includes(filter)) return;
        setSourceLoading(true); setError("");
        try {
            const hasUnfilteredSource = source.filterSubjects.includes("");
            const res = await fetch(withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}/sources`), {
                method: hasUnfilteredSource ? "PATCH" : "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
                body: JSON.stringify({
                    sourceAccountPublicKey: source.accountPublicKey,
                    sourceName: source.name,
                    ...(hasUnfilteredSource ? { currentFilterSubject: "" } : {}),
                    filterSubject: filter,
                }),
            });
            if (res.status === 401) { onAuthError(); return; }
            if (!res.ok) { const data = (await res.json().catch(() => ({}))) as { error?: string }; setError(data.error ?? "Failed to add source filter."); return; }
            const streamRes = await fetch(withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}`), { headers: { Authorization: `Bearer ${token}` } });
            if (streamRes.ok) { const data = (await streamRes.json()) as StreamDetailType; setStream({ ...data, subjects: data.subjects ?? [] }); }
            setSourceCardFilterInputs((prev) => ({ ...prev, [key]: "" }));
        } finally { setSourceLoading(false); }
    };

    const handleRemoveFilterFromSource = async (source: SourceStream, filter: string) => {
        if (!window.confirm(`Remove filter '${filter}' from source '${source.name}'?`)) return;
        setSourceLoading(true); setError("");
        try {
            const res = await fetch(withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}/sources`), {
                method: "DELETE", headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
                body: JSON.stringify({ sourceAccountPublicKey: source.accountPublicKey, sourceName: source.name, filterSubject: filter }),
            });
            if (res.status === 401) { onAuthError(); return; }
            if (!res.ok) { const data = (await res.json().catch(() => ({}))) as { error?: string }; setError(data.error ?? "Failed to remove source filter."); return; }
            const streamRes = await fetch(withAccountScope(`${apiBase}/api/v1/streams/${encodeURIComponent(streamName)}`), { headers: { Authorization: `Bearer ${token}` } });
            if (streamRes.ok) { const data = (await streamRes.json()) as StreamDetailType; setStream({ ...data, subjects: data.subjects ?? [] }); }
        } finally { setSourceLoading(false); }
    };


    return (
        <section className="panel">
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <h3>{stream.name}</h3>
                <button
                    type="button"
                    onClick={handlePurge}
                    style={{ background: "#b00020", color: "white", border: "none", padding: "0.4rem 0.6rem", borderRadius: 4 }}
                >
                    Purge
                </button>
            </div>
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

                <div>
                    <strong>Source Streams:</strong>
                    {sources.length === 0 ? (
                        <span
                            className="muted"
                            style={{ marginLeft: "0.5rem", fontSize: "0.85em" }}
                        >
                            none
                        </span>
                    ) : (
                        <ul className="list">
                            {sources.map((source) => (
                                <li key={sourceKey(source)} className="list-row">
                                    <div>
                                        <code>{source.name}</code>
                                        {source.filterSubjects.length === 1 && source.filterSubjects[0] === "" && <span className="muted" style={{ marginLeft: "0.5rem" }}>no filter</span>}
                                    </div>
                                    {source.filterSubjects.filter(Boolean).length > 0 && (
                                        <ul className="list" style={{ width: "100%", marginTop: "0.4rem" }}>
                                            {source.filterSubjects.filter(Boolean).map((filter) => (
                                                <li key={filter} className="list-row">
                                                    <code>{filter}</code>
                                                    <button type="button" className="delete-btn" disabled={sourceLoading} onClick={() => handleRemoveFilterFromSource(source, filter)}>✕</button>
                                                </li>
                                            ))}
                                        </ul>
                                    )}
                                    <div style={{ display: "flex", gap: "0.4rem", width: "100%", marginTop: "0.4rem" }}>
                                        <input
                                            value={sourceCardFilterInputs[sourceKey(source)] ?? ""}
                                            onChange={(e) => setSourceCardFilterInputs((prev) => ({ ...prev, [sourceKey(source)]: e.target.value }))}
                                            placeholder="Subject filter"
                                            disabled={sourceLoading}
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") {
                                                    e.preventDefault();
                                                    handleAddFilterToSource(source);
                                                }
                                            }}
                                        />
                                        <button type="button" disabled={sourceLoading || !(sourceCardFilterInputs[sourceKey(source)] ?? "").trim()} onClick={() => handleAddFilterToSource(source)}>{sourceLoading ? "Adding…" : "Add filter"}</button>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>

                {consumers.length === 0 && (
                    <div className="stack">
                        <strong>{sources.length === 0 ? "Add Source" : "Add Another Source"}</strong>
                        <select value={sourceAccountPublicKey} onChange={(e) => setSourceAccountPublicKey(e.target.value)}>
                            <option value="">Select source account…</option>
                            {sourceAccounts.map((account) => <option key={account.publicKey} value={account.publicKey}>{account.name}</option>)}
                        </select>
                        <select value={sourceName} onChange={(e) => setSourceName(e.target.value)} disabled={!sourceAccountPublicKey}>
                            <option value="">Select source stream…</option>
                            {sourceStreams.filter((name) => !(sourceAccountPublicKey === accountPublicKey && name === streamName)).map((name) => <option key={name} value={name}>{name}</option>)}
                        </select>
                        <div style={{ display: "flex", gap: "0.4rem" }}>
                            <input
                                value={sourceFilterInput}
                                onChange={(e) => setSourceFilterInput(e.target.value)}
                                placeholder="Subject filter (optional, e.g. orders.created)"
                                disabled={sourceLoading}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        handleAddSourceFilter();
                                    }
                                }}
                            />
                            <button type="button" disabled={sourceLoading || !sourceFilterInput.trim()} onClick={handleAddSourceFilter}>Add filter</button>
                        </div>
                        {sourceFilters.length > 0 && (
                            <ul className="list">
                                {sourceFilters.map((filter) => (
                                    <li key={filter} className="list-row">
                                        <code>{filter}</code>
                                        <button type="button" className="delete-btn" disabled={sourceLoading} onClick={() => setSourceFilters((prev) => prev.filter((value) => value !== filter))}>✕</button>
                                    </li>
                                ))}
                            </ul>
                        )}
                        <button type="button" disabled={sourceLoading || !sourceName} onClick={handleAddSource}>{sourceLoading ? "Adding…" : "Add Source"}</button>
                    </div>
                )}

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
