import { useState } from "react";
import type { User } from "../../types";

type Props = {
    accountKey: string;
    users: User[];
    onCreateUser: (name: string) => Promise<void>;
    onDeleteUser: (name: string) => Promise<void>;
    onAddPublishAllow: (userName: string, subject: string) => Promise<void>;
    onRemovePublishAllow: (userName: string, subject: string) => Promise<void>;
    onAddPublishDeny?: (userName: string, subject: string) => Promise<void>;
    onRemovePublishDeny?: (userName: string, subject: string) => Promise<void>;
    onGetCreds: (userName: string) => Promise<string>;
};

type CredsModal = {
    userName: string;
    creds: string;
    loading: boolean;
    error: string;
};

export default function UserList({
    accountKey,
    users,
    onCreateUser,
    onDeleteUser,
    onAddPublishAllow,
    onRemovePublishAllow,
    onAddPublishDeny,
    onGetCreds,
}: Props) {
    const [newUserName, setNewUserName] = useState("");
    const [subjectInputs, setSubjectInputs] = useState<Record<string, string>>(
        {},
    );
    const [credsModal, setCredsModal] = useState<CredsModal | null>(null);

    const handleCreateUser = async () => {
        const name = newUserName.trim();
        if (!name) return;
        await onCreateUser(name);
        setNewUserName("");
    };

    const handleAddSubject = async (userName: string) => {
        const subject = (subjectInputs[userName] ?? "").trim();
        if (!subject) return;
        await onAddPublishAllow(userName, subject);
        setSubjectInputs((prev) => ({ ...prev, [userName]: "" }));
    };

    const handleShowCreds = async (userName: string) => {
        setCredsModal({ userName, creds: "", loading: true, error: "" });
        try {
            const creds = await onGetCreds(userName);
            setCredsModal({ userName, creds, loading: false, error: "" });
        } catch (err) {
            setCredsModal({
                userName,
                creds: "",
                loading: false,
                error:
                    err instanceof Error ? err.message : "Failed to load creds",
            });
        }
    };

    const handleDownloadCreds = (userName: string, creds: string) => {
        const blob = new Blob([creds], { type: "text/plain;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${userName}.creds`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
    };

    return (
        <div style={{ marginTop: "0.75rem" }}>
            <strong style={{ fontSize: "0.85em" }}>Users:</strong>
            {users.length === 0 && (
                <span
                    className="muted"
                    style={{ marginLeft: "0.5rem", fontSize: "0.85em" }}
                >
                    none
                </span>
            )}
            <ul className="list" style={{ marginTop: "0.3rem" }}>
                {users.map((u) => (
                    <li
                        key={`${accountKey}/${u.name}`}
                        style={{
                            paddingLeft: "0.5rem",
                            borderTop: "1px solid var(--border, #e2e8f0)",
                            paddingTop: "0.4rem",
                            marginTop: "0.4rem",
                        }}
                    >
                        <div
                            style={{
                                display: "flex",
                                alignItems: "center",
                                gap: "0.5rem",
                                marginBottom: "0.2rem",
                            }}
                        >
                            <strong style={{ fontSize: "0.85em" }}>
                                {u.name}
                            </strong>
                            <button
                                type="button"
                                onClick={() => handleShowCreds(u.name)}
                                style={{
                                    marginLeft: "auto",
                                    fontSize: "0.75em",
                                    padding: "0 0.3rem",
                                }}
                            >
                                Creds
                            </button>
                            <button
                                type="button"
                                onClick={() => onDeleteUser(u.name)}
                                style={{
                                    fontSize: "0.75em",
                                    padding: "0 0.3rem",
                                }}
                            >
                                Delete
                            </button>
                        </div>
                        {u.publicKey && (
                            <div
                                style={{
                                    fontSize: "0.75em",
                                    color: "var(--muted, #64748b)",
                                    paddingLeft: "0.25rem",
                                    marginBottom: "0.2rem",
                                    wordBreak: "break-all",
                                }}
                            >
                                <span style={{ marginRight: "0.3rem" }}>
                                    pub:
                                </span>
                                <code>{u.publicKey}</code>
                            </div>
                        )}
                        <div
                            style={{
                                display: "flex",
                                alignItems: "center",
                                gap: "0.4rem",
                                flexWrap: "wrap",
                                paddingLeft: "0.25rem",
                            }}
                        >
                            <span
                                style={{
                                    fontSize: "0.78em",
                                    color: "var(--muted, #64748b)",
                                }}
                            >
                                subjects:
                            </span>
                            {u.publishAllow.length === 0 && (
                                <span
                                    style={{
                                        fontSize: "0.78em",
                                        color: "var(--muted, #64748b)",
                                    }}
                                >
                                    —
                                </span>
                            )}
                            {u.publishAllow.map((sub) => (
                                <span
                                    key={sub}
                                    style={{
                                        fontSize: "0.78em",
                                        display: "inline-flex",
                                        alignItems: "center",
                                        gap: "0.2rem",
                                        background: "#dbeafe",
                                        color: "#1e40af",
                                        borderRadius: "3px",
                                        padding: "0 0.3rem",
                                    }}
                                >
                                    <code style={{ background: "none" }}>
                                        {sub}
                                    </code>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            onRemovePublishAllow(u.name, sub)
                                        }
                                        style={{
                                            fontSize: "0.7em",
                                            padding: "0 0.2rem",
                                            lineHeight: 1,
                                            background: "none",
                                            border: "none",
                                            cursor: "pointer",
                                            color: "inherit",
                                        }}
                                    >
                                        ✕
                                    </button>
                                </span>
                            ))}
                            {u.name === "stream-reader" && (
                                <div style={{ marginLeft: "0.5rem" }}>
                                    {/* Only allow revoking purge (adding the deny). Do not show a "Grant Purge" button. */}
                                    {(!u.publishDeny || !u.publishDeny.includes("$JS.API.STREAM.PURGE.>")) && (
                                        <button
                                            type="button"
                                            onClick={() =>
                                                onAddPublishDeny &&
                                                onAddPublishDeny(u.name, "$JS.API.STREAM.PURGE.>")
                                            }
                                            style={{ fontSize: "0.78em" }}
                                        >
                                            Revoke Purge
                                        </button>
                                    )}
                                </div>
                            )}
                        </div>
                        <div
                            style={{
                                display: "flex",
                                gap: "0.4rem",
                                marginTop: "0.3rem",
                            }}
                        >
                            <input
                                value={subjectInputs[u.name] ?? ""}
                                onChange={(e) =>
                                    setSubjectInputs((prev) => ({
                                        ...prev,
                                        [u.name]: e.target.value,
                                    }))
                                }
                                placeholder="add pub subject"
                                style={{ fontSize: "0.8em" }}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        handleAddSubject(u.name);
                                    }
                                }}
                            />
                            <button
                                type="button"
                                onClick={() => handleAddSubject(u.name)}
                                style={{ fontSize: "0.8em" }}
                            >
                                Add
                            </button>
                        </div>
                    </li>
                ))}
            </ul>
            <div
                style={{ display: "flex", gap: "0.4rem", marginTop: "0.4rem" }}
            >
                <input
                    value={newUserName}
                    onChange={(e) => setNewUserName(e.target.value)}
                    placeholder="user name"
                    style={{ fontSize: "0.85em" }}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault();
                            handleCreateUser();
                        }
                    }}
                />
                <button
                    type="button"
                    onClick={handleCreateUser}
                    style={{ fontSize: "0.85em" }}
                >
                    Add User
                </button>
            </div>
            {credsModal && (
                <div
                    style={{
                        position: "fixed",
                        inset: 0,
                        background: "rgba(0,0,0,0.45)",
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        zIndex: 1000,
                    }}
                    onClick={() => setCredsModal(null)}
                >
                    <div
                        style={{
                            background: "var(--panel-bg, #fff)",
                            border: "1px solid var(--border, #e2e8f0)",
                            borderRadius: "8px",
                            padding: "1.5rem",
                            maxWidth: "540px",
                            width: "90%",
                            maxHeight: "80vh",
                            display: "flex",
                            flexDirection: "column",
                            gap: "0.75rem",
                            overflowY: "auto",
                        }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <h3 style={{ margin: 0 }}>
                            Creds — {credsModal.userName}
                        </h3>
                        {credsModal.loading && (
                            <p className="muted">Generating…</p>
                        )}
                        {credsModal.error && (
                            <p className="error">{credsModal.error}</p>
                        )}
                        {credsModal.creds && (
                            <>
                                <p className="muted" style={{ margin: 0 }}>
                                    Keep this file secure — it grants NATS
                                    access.
                                </p>
                                <textarea
                                    readOnly
                                    value={credsModal.creds}
                                    style={{
                                        width: "100%",
                                        minHeight: "14rem",
                                        fontFamily: "monospace",
                                        fontSize: "0.78rem",
                                        resize: "vertical",
                                        boxSizing: "border-box",
                                    }}
                                />
                                <div style={{ display: "flex", gap: "0.5rem" }}>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            navigator.clipboard.writeText(
                                                credsModal.creds,
                                            )
                                        }
                                    >
                                        Copy
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            handleDownloadCreds(
                                                credsModal.userName,
                                                credsModal.creds,
                                            )
                                        }
                                    >
                                        Download .creds
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => setCredsModal(null)}
                                        style={{ marginLeft: "auto" }}
                                    >
                                        Close
                                    </button>
                                </div>
                            </>
                        )}
                        {!credsModal.loading && !credsModal.creds && (
                            <button
                                type="button"
                                onClick={() => setCredsModal(null)}
                            >
                                Close
                            </button>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}
