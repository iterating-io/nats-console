import { useState } from "react";
import UserList from "./UserList";
import type { Account } from "../../types";
import {
    accountHasJSConsumerApiAccess,
    isJSConsumerApiPublishSubject,
    isJSConsumerApiSubscribeSubject,
} from "../../constants/jsApi";

type Props = {
    accounts: Account[];
    canDeleteAccounts: boolean;
    onDeleteAccount: (
        operator: string,
        accountPublicKey: string,
    ) => Promise<void>;
    onAddPublishAllow: (
        operator: string,
        name: string,
        subject: string,
    ) => Promise<void>;
    onRemovePublishAllow: (
        operator: string,
        name: string,
        subject: string,
    ) => Promise<void>;
    onAddSubscribeAllow: (
        operator: string,
        name: string,
        subject: string,
    ) => Promise<void>;
    onRemoveSubscribeAllow: (
        operator: string,
        name: string,
        subject: string,
    ) => Promise<void>;
    onCreateUser: (
        operator: string,
        accountPublicKey: string,
        userName: string,
    ) => Promise<void>;
    onDeleteUser: (
        operator: string,
        accountPublicKey: string,
        userName: string,
    ) => Promise<void>;
    onAddUserPublishAllow: (
        operator: string,
        accountPublicKey: string,
        userName: string,
        subject: string,
    ) => Promise<void>;
    onRemoveUserPublishAllow: (
        operator: string,
        accountPublicKey: string,
        userName: string,
        subject: string,
    ) => Promise<void>;
    onGetUserCreds: (
        operator: string,
        accountPublicKey: string,
        userName: string,
    ) => Promise<string>;
    onGetAccountJWT: (
        operator: string,
        accountPublicKey: string,
    ) => Promise<{ jwt: string; payload: Record<string, unknown> }>;
    onToggleJetStream: (
        operator: string,
        accountPublicKey: string,
        enabled: boolean,
    ) => Promise<void>;
    onGrantJSConsumerApiAccess: (
        operator: string,
        name: string,
    ) => Promise<void>;
    onRevokeJSConsumerApiAccess: (
        operator: string,
        name: string,
    ) => Promise<void>;
};

export default function AccountList({
    accounts,
    canDeleteAccounts,
    onDeleteAccount,
    onAddPublishAllow,
    onRemovePublishAllow,
    onAddSubscribeAllow,
    onRemoveSubscribeAllow,
    onCreateUser,
    onDeleteUser,
    onAddUserPublishAllow,
    onRemoveUserPublishAllow,
    onGetUserCreds,
    onGetAccountJWT,
    onToggleJetStream,
    onGrantJSConsumerApiAccess,
    onRevokeJSConsumerApiAccess,
}: Props) {
    const [subjectInputs, setSubjectInputs] = useState<Record<string, string>>(
        {},
    );
    const [subscribeInputs, setSubscribeInputs] = useState<
        Record<string, string>
    >({});
    const [deleteCandidate, setDeleteCandidate] = useState<Account | null>(
        null,
    );
    const [jwtModal, setJwtModal] = useState<{
        raw: string;
        payload: Record<string, unknown>;
    } | null>(null);
    const [jwtLoading, setJwtLoading] = useState<string | null>(null);
    const [jsToggleLoading, setJsToggleLoading] = useState<string | null>(null);
    const [jsApiLoading, setJsApiLoading] = useState<string | null>(null);

    const key = (acc: Account) => `${acc.operator}/${acc.publicKey}`;

    const handleAdd = async (acc: Account) => {
        const k = key(acc);
        const subject = (subjectInputs[k] ?? "").trim();
        if (!subject) return;
        await onAddPublishAllow(acc.operator, acc.name, subject);
        setSubjectInputs((prev) => ({ ...prev, [k]: "" }));
    };

    const handleAddSubscribe = async (acc: Account) => {
        const k = key(acc);
        const subject = (subscribeInputs[k] ?? "").trim();
        if (!subject) return;
        await onAddSubscribeAllow(acc.operator, acc.name, subject);
        setSubscribeInputs((prev) => ({ ...prev, [k]: "" }));
    };

    return (
        <div>
            <ul className="list">
                {accounts.length === 0 && (
                    <li className="muted">
                        No account is available for the selected operator.
                    </li>
                )}
                {accounts.map((acc) => {
                    const visiblePublishAllow = acc.publishAllow.filter(
                        (subject) => !isJSConsumerApiPublishSubject(subject),
                    );
                    const visibleSubscribeAllow = acc.subscribeAllow.filter(
                        (subject) => !isJSConsumerApiSubscribeSubject(subject),
                    );

                    return (
                        <li key={key(acc)}>
                            <div
                                style={{
                                    display: "flex",
                                    alignItems: "center",
                                    gap: "0.5rem",
                                }}
                            >
                                <span style={{ fontWeight: 600 }}>
                                    {acc.name}
                                </span>
                                {acc.jsEnabled && (
                                    <span className="badge ok">JS</span>
                                )}
                                <span className="muted"> — {acc.operator}</span>
                                <code
                                    className="muted"
                                    style={{ fontSize: "0.75em" }}
                                >
                                    {acc.publicKey}
                                </code>
                                <button
                                    type="button"
                                    disabled={jwtLoading === key(acc)}
                                    onClick={async () => {
                                        setJwtLoading(key(acc));
                                        try {
                                            const data = await onGetAccountJWT(
                                                acc.operator,
                                                acc.publicKey,
                                            );
                                            setJwtModal({
                                                raw: data.jwt,
                                                payload: data.payload,
                                            });
                                        } finally {
                                            setJwtLoading(null);
                                        }
                                    }}
                                    style={{
                                        marginLeft: "auto",
                                        fontSize: "0.75em",
                                        padding: "0 0.3rem",
                                    }}
                                >
                                    {jwtLoading === key(acc) ? "…" : "View JWT"}
                                </button>
                                <button
                                    type="button"
                                    disabled={jsToggleLoading === key(acc)}
                                    onClick={async () => {
                                        setJsToggleLoading(key(acc));
                                        try {
                                            await onToggleJetStream(
                                                acc.operator,
                                                acc.publicKey,
                                                !acc.jsEnabled,
                                            );
                                        } finally {
                                            setJsToggleLoading(null);
                                        }
                                    }}
                                    style={{
                                        fontSize: "0.75em",
                                        padding: "0 0.3rem",
                                    }}
                                >
                                    {jsToggleLoading === key(acc)
                                        ? "…"
                                        : acc.jsEnabled
                                          ? "Disable JS"
                                          : "Enable JS"}
                                </button>
                                <button
                                    type="button"
                                    disabled={jsApiLoading === key(acc)}
                                    onClick={async () => {
                                        setJsApiLoading(key(acc));
                                        try {
                                            if (
                                                accountHasJSConsumerApiAccess(
                                                    acc,
                                                )
                                            ) {
                                                await onRevokeJSConsumerApiAccess(
                                                    acc.operator,
                                                    acc.name,
                                                );
                                            } else {
                                                await onGrantJSConsumerApiAccess(
                                                    acc.operator,
                                                    acc.name,
                                                );
                                            }
                                        } finally {
                                            setJsApiLoading(null);
                                        }
                                    }}
                                    style={{
                                        fontSize: "0.75em",
                                        padding: "0 0.3rem",
                                    }}
                                >
                                    {jsApiLoading === key(acc)
                                        ? "…"
                                        : accountHasJSConsumerApiAccess(acc)
                                          ? "Revoke JS API"
                                          : "Grant JS API"}
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setDeleteCandidate(acc)}
                                    disabled={!canDeleteAccounts}
                                    style={{
                                        fontSize: "0.75em",
                                        padding: "0 0.3rem",
                                    }}
                                >
                                    Delete
                                </button>
                            </div>
                            <div style={{ marginTop: "0.4rem" }}>
                                <strong style={{ fontSize: "0.85em" }}>
                                    Publish allow:
                                </strong>
                                {visiblePublishAllow.length === 0 && (
                                    <span
                                        className="muted"
                                        style={{
                                            marginLeft: "0.5rem",
                                            fontSize: "0.85em",
                                        }}
                                    >
                                        none
                                    </span>
                                )}
                                <ul
                                    style={{
                                        margin: "0.2rem 0 0.4rem 1rem",
                                        padding: 0,
                                        listStyle: "disc",
                                    }}
                                >
                                    {visiblePublishAllow.map((sub) => (
                                        <li
                                            key={sub}
                                            style={{
                                                fontSize: "0.85em",
                                                display: "flex",
                                                alignItems: "center",
                                                gap: "0.4rem",
                                            }}
                                        >
                                            <code>{sub}</code>
                                            <button
                                                type="button"
                                                onClick={() =>
                                                    onRemovePublishAllow(
                                                        acc.operator,
                                                        acc.name,
                                                        sub,
                                                    )
                                                }
                                                style={{
                                                    fontSize: "0.75em",
                                                    padding: "0 0.3rem",
                                                }}
                                            >
                                                ✕
                                            </button>
                                        </li>
                                    ))}
                                </ul>
                                <div style={{ display: "flex", gap: "0.4rem" }}>
                                    <input
                                        value={subjectInputs[key(acc)] ?? ""}
                                        onChange={(e) =>
                                            setSubjectInputs((prev) => ({
                                                ...prev,
                                                [key(acc)]: e.target.value,
                                            }))
                                        }
                                        placeholder="subject (e.g. orders.>)"
                                        style={{ fontSize: "0.85em" }}
                                        onKeyDown={(e) => {
                                            if (e.key === "Enter") {
                                                e.preventDefault();
                                                handleAdd(acc);
                                            }
                                        }}
                                    />
                                    <button
                                        type="button"
                                        onClick={() => handleAdd(acc)}
                                        style={{ fontSize: "0.85em" }}
                                    >
                                        Add
                                    </button>
                                </div>
                            </div>
                            <div style={{ marginTop: "0.4rem" }}>
                                <strong style={{ fontSize: "0.85em" }}>
                                    Subscribe allow:
                                </strong>
                                {visibleSubscribeAllow.length === 0 && (
                                    <span
                                        className="muted"
                                        style={{
                                            marginLeft: "0.5rem",
                                            fontSize: "0.85em",
                                        }}
                                    >
                                        none
                                    </span>
                                )}
                                <ul
                                    style={{
                                        margin: "0.2rem 0 0.4rem 1rem",
                                        padding: 0,
                                        listStyle: "disc",
                                    }}
                                >
                                    {visibleSubscribeAllow.map((sub) => (
                                        <li
                                            key={sub}
                                            style={{
                                                fontSize: "0.85em",
                                                display: "flex",
                                                alignItems: "center",
                                                gap: "0.4rem",
                                            }}
                                        >
                                            <code>{sub}</code>
                                            <button
                                                type="button"
                                                onClick={() =>
                                                    onRemoveSubscribeAllow(
                                                        acc.operator,
                                                        acc.name,
                                                        sub,
                                                    )
                                                }
                                                style={{
                                                    fontSize: "0.75em",
                                                    padding: "0 0.3rem",
                                                }}
                                            >
                                                ✕
                                            </button>
                                        </li>
                                    ))}
                                </ul>
                                <div style={{ display: "flex", gap: "0.4rem" }}>
                                    <input
                                        value={subscribeInputs[key(acc)] ?? ""}
                                        onChange={(e) =>
                                            setSubscribeInputs((prev) => ({
                                                ...prev,
                                                [key(acc)]: e.target.value,
                                            }))
                                        }
                                        placeholder="subject (e.g. updates.>)"
                                        style={{ fontSize: "0.85em" }}
                                        onKeyDown={(e) => {
                                            if (e.key === "Enter") {
                                                e.preventDefault();
                                                handleAddSubscribe(acc);
                                            }
                                        }}
                                    />
                                    <button
                                        type="button"
                                        onClick={() => handleAddSubscribe(acc)}
                                        style={{ fontSize: "0.85em" }}
                                    >
                                        Add
                                    </button>
                                </div>
                            </div>
                            <UserList
                                accountKey={key(acc)}
                                users={acc.users}
                                onCreateUser={(userName) =>
                                    onCreateUser(
                                        acc.operator,
                                        acc.publicKey,
                                        userName,
                                    )
                                }
                                onDeleteUser={(userName) =>
                                    onDeleteUser(
                                        acc.operator,
                                        acc.publicKey,
                                        userName,
                                    )
                                }
                                onAddPublishAllow={(userName, subject) =>
                                    onAddUserPublishAllow(
                                        acc.operator,
                                        acc.publicKey,
                                        userName,
                                        subject,
                                    )
                                }
                                onRemovePublishAllow={(userName, subject) =>
                                    onRemoveUserPublishAllow(
                                        acc.operator,
                                        acc.publicKey,
                                        userName,
                                        subject,
                                    )
                                }
                                onGetCreds={(userName) =>
                                    onGetUserCreds(
                                        acc.operator,
                                        acc.publicKey,
                                        userName,
                                    )
                                }
                            />
                        </li>
                    );
                })}
            </ul>
            {deleteCandidate && (
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
                    onClick={() => setDeleteCandidate(null)}
                >
                    <div
                        style={{
                            background: "var(--panel-bg, #fff)",
                            border: "1px solid var(--border, #e2e8f0)",
                            borderRadius: "8px",
                            padding: "1rem",
                            width: "90%",
                            maxWidth: "420px",
                            display: "flex",
                            flexDirection: "column",
                            gap: "0.75rem",
                        }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <h3 style={{ margin: 0 }}>Delete Account</h3>
                        <p className="muted" style={{ margin: 0 }}>
                            Delete <strong>{deleteCandidate.name}</strong>{" "}
                            account and all linked users?
                        </p>
                        <div style={{ display: "flex", gap: "0.5rem" }}>
                            <button
                                type="button"
                                onClick={() => setDeleteCandidate(null)}
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={async () => {
                                    await onDeleteAccount(
                                        deleteCandidate.operator,
                                        deleteCandidate.publicKey,
                                    );
                                    setDeleteCandidate(null);
                                }}
                            >
                                Delete
                            </button>
                        </div>
                    </div>
                </div>
            )}
            {jwtModal && (
                <div
                    style={{
                        position: "fixed",
                        inset: 0,
                        background: "rgba(0,0,0,0.55)",
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        zIndex: 1100,
                    }}
                    onClick={() => setJwtModal(null)}
                >
                    <div
                        style={{
                            background: "var(--panel-bg, #1e1e2e)",
                            border: "1px solid var(--border, #333)",
                            borderRadius: "8px",
                            padding: "1.25rem",
                            width: "90%",
                            maxWidth: "700px",
                            maxHeight: "80vh",
                            display: "flex",
                            flexDirection: "column",
                            gap: "0.75rem",
                            overflow: "hidden",
                        }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div
                            style={{
                                display: "flex",
                                alignItems: "center",
                                justifyContent: "space-between",
                            }}
                        >
                            <h3 style={{ margin: 0 }}>Account JWT</h3>
                            <button
                                type="button"
                                onClick={() => setJwtModal(null)}
                                style={{
                                    fontSize: "0.85em",
                                    padding: "0 0.4rem",
                                }}
                            >
                                Close
                            </button>
                        </div>
                        <div>
                            <strong style={{ fontSize: "0.85em" }}>
                                Raw JWT
                            </strong>
                            <div
                                style={{
                                    marginTop: "0.3rem",
                                    background: "rgba(0,0,0,0.25)",
                                    borderRadius: "4px",
                                    padding: "0.5rem",
                                    fontSize: "0.72em",
                                    wordBreak: "break-all",
                                    fontFamily: "monospace",
                                    maxHeight: "6rem",
                                    overflowY: "auto",
                                }}
                            >
                                {jwtModal.raw}
                            </div>
                        </div>
                        <div
                            style={{
                                flex: 1,
                                overflow: "hidden",
                                display: "flex",
                                flexDirection: "column",
                            }}
                        >
                            <strong style={{ fontSize: "0.85em" }}>
                                Payload
                            </strong>
                            <pre
                                style={{
                                    marginTop: "0.3rem",
                                    background: "rgba(0,0,0,0.25)",
                                    borderRadius: "4px",
                                    padding: "0.75rem",
                                    fontSize: "0.8em",
                                    overflowY: "auto",
                                    flex: 1,
                                    whiteSpace: "pre-wrap",
                                    wordBreak: "break-word",
                                }}
                            >
                                {JSON.stringify(jwtModal.payload, null, 2)}
                            </pre>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
