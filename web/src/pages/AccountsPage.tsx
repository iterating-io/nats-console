import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../context/AuthContext";
import { useApiBase } from "../hooks/useApiBase";
import AccountList from "../components/Account/AccountList";
import AccountForm from "../components/Account/AccountForm";
import OperatorList from "../components/Operator/OperatorList";
import type { Account, User } from "../types";
import {
    JS_CONSUMER_API_PUBLISH,
    JS_CONSUMER_API_SUBSCRIBE,
} from "../constants/jsApi";

type AccountCapabilities = {
    accountDelete?: boolean;
};

export default function AccountsPage() {
    const apiBase = useApiBase();
    const navigate = useNavigate();
    const { token, logout } = useAuth();
    const [accounts, setAccounts] = useState<Account[]>([]);
    const [operators, setOperators] = useState<string[]>([]);
    const [selectedOperator, setSelectedOperator] = useState("");
    const [error, setError] = useState("");
    const [capabilities, setCapabilities] =
        useState<AccountCapabilities | null>(null);

    const fetchAll = async () => {
        const [opRes, accRes] = await Promise.all([
            fetch(`${apiBase}/api/v1/operators`, {
                headers: { Authorization: `Bearer ${token}` },
            }),
            fetch(`${apiBase}/api/v1/accounts`, {
                headers: { Authorization: `Bearer ${token}` },
            }),
        ]);
        if (opRes.status === 401 || accRes.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (opRes.ok) {
            const data = (await opRes.json()) as {
                operators: { name: string }[];
            };
            setOperators(data.operators.map((o) => o.name));
        }
        if (accRes.ok) {
            const data = (await accRes.json()) as {
                accounts: Account[];
                capabilities?: AccountCapabilities;
            };
            setCapabilities(data.capabilities ?? { accountDelete: false });
            const baseAccounts = data.accounts.map((a) => ({
                ...a,
                publishAllow: a.publishAllow ?? [],
                subscribeAllow: a.subscribeAllow ?? [],
                jsEnabled: a.jsEnabled ?? false,
                users: [] as User[],
            }));
            const usersResults = await Promise.all(
                baseAccounts.map((a) =>
                    fetch(
                        `${apiBase}/api/v1/accounts/${encodeURIComponent(a.operator)}/${encodeURIComponent(a.publicKey)}/users`,
                        { headers: { Authorization: `Bearer ${token}` } },
                    ).then((r) =>
                        r.ok
                            ? (r.json() as Promise<{ users: User[] }>)
                            : Promise.resolve({ users: [] as User[] }),
                    ),
                ),
            );
            setAccounts(
                baseAccounts.map((a, i) => ({
                    ...a,
                    users: (usersResults[i].users ?? []).map((u) => ({
                        ...u,
                        publicKey: u.publicKey ?? "",
                        publishAllow: u.publishAllow ?? [],
                        publishDeny: u.publishDeny ?? [],
                    })),
                })),
            );
        }
    };

    useEffect(() => {
        fetchAll().catch(() => setError("Failed to load data."));
    }, []);

    useEffect(() => {
        if (selectedOperator && operators.includes(selectedOperator)) {
            return;
        }
        setSelectedOperator(operators[0] ?? "");
    }, [operators, selectedOperator]);

    const handleSelectOperator = (name: string) => {
        setSelectedOperator(name);
    };

    const handleCreated = async (
        name: string,
        publishAllow: string[],
        subscribeAllow: string[],
    ) => {
        setError("");
        if (!selectedOperator) {
            setError("Please select an operator first.");
            return;
        }
        const res = await fetch(`${apiBase}/api/v1/accounts`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                Authorization: `Bearer ${token}`,
            },
            body: JSON.stringify({
                operator: selectedOperator,
                name,
                publishAllow,
                subscribeAllow,
            }),
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
            setError(data.error ?? "Failed to create account.");
            return;
        }
        await fetchAll();
    };

    const handleDeleteAccount = async (
        operator: string,
        accountPublicKey: string,
    ) => {
        setError("");
        if (capabilities === null || capabilities.accountDelete === false) {
            setError("Account delete is disabled by the NATS resolver.");
            return;
        }
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}`,
            {
                method: "DELETE",
                headers: { Authorization: `Bearer ${token}` },
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to delete account.");
            return;
        }
        await fetchAll();
    };

    const handleAddPublishAllow = async (
        operator: string,
        name: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/publish-allow`,
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to add subject.");
            return;
        }
        await fetchAll();
    };

    const handleRemovePublishAllow = async (
        operator: string,
        name: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/publish-allow`,
            {
                method: "DELETE",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to remove subject.");
            return;
        }
        await fetchAll();
    };

    const handleAddSubscribeAllow = async (
        operator: string,
        name: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/subscribe-allow`,
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to add subject.");
            return;
        }
        await fetchAll();
    };

    const handleRemoveSubscribeAllow = async (
        operator: string,
        name: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/subscribe-allow`,
            {
                method: "DELETE",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to remove subject.");
            return;
        }
        await fetchAll();
    };

    const handleCreateUser = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users`,
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({
                    name: userName,
                }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to create user.");
            return;
        }
        await fetchAll();
    };

    const handleGetAccountJWT = async (
        operator: string,
        accountPublicKey: string,
    ): Promise<{ jwt: string; payload: Record<string, unknown> }> => {
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/jwt`,
            { headers: { Authorization: `Bearer ${token}` } },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            throw new Error("Unauthorized");
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            throw new Error(data.error ?? "Failed to get account JWT");
        }
        return res.json() as Promise<{
            jwt: string;
            payload: Record<string, unknown>;
        }>;
    };

    const handleToggleJetStream = async (
        operator: string,
        accountPublicKey: string,
        enabled: boolean,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/jetstream`,
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ enabled }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(
                data.error ??
                    `Failed to ${enabled ? "enable" : "disable"} JetStream for account.`,
            );
            return;
        }
        await fetchAll();
    };

    const handleGrantJSConsumerApiAccess = async (
        operator: string,
        name: string,
    ) => {
        setError("");
        for (const subject of JS_CONSUMER_API_PUBLISH) {
            const res = await fetch(
                `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/publish-allow`,
                {
                    method: "POST",
                    headers: {
                        "Content-Type": "application/json",
                        Authorization: `Bearer ${token}`,
                    },
                    body: JSON.stringify({ subject }),
                },
            );
            if (res.status === 401) {
                logout();
                navigate("/");
                return;
            }
            if (!res.ok) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                setError(
                    data.error ?? "Failed to grant JS Consumer API access.",
                );
                return;
            }
        }
        for (const subject of JS_CONSUMER_API_SUBSCRIBE) {
            const res = await fetch(
                `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/subscribe-allow`,
                {
                    method: "POST",
                    headers: {
                        "Content-Type": "application/json",
                        Authorization: `Bearer ${token}`,
                    },
                    body: JSON.stringify({ subject }),
                },
            );
            if (res.status === 401) {
                logout();
                navigate("/");
                return;
            }
            if (!res.ok) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                setError(
                    data.error ?? "Failed to grant JS Consumer API access.",
                );
                return;
            }
        }
        await fetchAll();
    };

    const handleRevokeJSConsumerApiAccess = async (
        operator: string,
        name: string,
    ) => {
        setError("");
        for (const subject of JS_CONSUMER_API_PUBLISH) {
            const res = await fetch(
                `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/publish-allow`,
                {
                    method: "DELETE",
                    headers: {
                        "Content-Type": "application/json",
                        Authorization: `Bearer ${token}`,
                    },
                    body: JSON.stringify({ subject }),
                },
            );
            if (res.status === 401) {
                logout();
                navigate("/");
                return;
            }
            if (!res.ok && res.status !== 204) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                setError(
                    data.error ?? "Failed to revoke JS Consumer API access.",
                );
                return;
            }
        }
        for (const subject of JS_CONSUMER_API_SUBSCRIBE) {
            const res = await fetch(
                `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(name)}/subscribe-allow`,
                {
                    method: "DELETE",
                    headers: {
                        "Content-Type": "application/json",
                        Authorization: `Bearer ${token}`,
                    },
                    body: JSON.stringify({ subject }),
                },
            );
            if (res.status === 401) {
                logout();
                navigate("/");
                return;
            }
            if (!res.ok && res.status !== 204) {
                const data = (await res.json().catch(() => ({}))) as {
                    error?: string;
                };
                setError(
                    data.error ?? "Failed to revoke JS Consumer API access.",
                );
                return;
            }
        }
        await fetchAll();
    };

    const handleGetUserCreds = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
    ): Promise<string> => {
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users/${encodeURIComponent(userName)}/creds`,
            { headers: { Authorization: `Bearer ${token}` } },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            throw new Error("Unauthorized");
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            throw new Error(data.error ?? "Failed to get creds");
        }
        const data = (await res.json()) as { creds: string };
        return data.creds;
    };

    const handleDeleteUser = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users/${encodeURIComponent(userName)}`,
            {
                method: "DELETE",
                headers: { Authorization: `Bearer ${token}` },
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to delete user.");
            return;
        }
        await fetchAll();
    };

    const handleAddUserPublishAllow = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users/${encodeURIComponent(userName)}/publish-allow`,
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to add subject.");
            return;
        }
        await fetchAll();
    };

    const handleAddUserPublishDeny = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users/${encodeURIComponent(userName)}/publish-deny`,
            {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to add deny subject.");
            return;
        }
        await fetchAll();
    };

    const handleRemoveUserPublishDeny = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users/${encodeURIComponent(userName)}/publish-deny`,
            {
                method: "DELETE",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to remove deny subject.");
            return;
        }
        await fetchAll();
    };

    const handleRemoveUserPublishAllow = async (
        operator: string,
        accountPublicKey: string,
        userName: string,
        subject: string,
    ) => {
        setError("");
        const res = await fetch(
            `${apiBase}/api/v1/accounts/${encodeURIComponent(operator)}/${encodeURIComponent(accountPublicKey)}/users/${encodeURIComponent(userName)}/publish-allow`,
            {
                method: "DELETE",
                headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${token}`,
                },
                body: JSON.stringify({ subject }),
            },
        );
        if (res.status === 401) {
            logout();
            navigate("/");
            return;
        }
        if (!res.ok) {
            const data = (await res.json().catch(() => ({}))) as {
                error?: string;
            };
            setError(data.error ?? "Failed to remove subject.");
            return;
        }
        await fetchAll();
    };

    const selectedAccounts = accounts.filter(
        (acc) => acc.operator === selectedOperator,
    );

    const [selectedAccountKey, setSelectedAccountKey] = useState("");

    const selectedAccount2 = selectedAccounts.find(
        (acc) => `${acc.operator}/${acc.publicKey}` === selectedAccountKey,
    );

    return (
        <div className="page-stack">
            <h2>Operators & Accounts</h2>
            <p className="notice">
                This page manages accounts and users for a NATS server running
                with a <strong>full resolver</strong>. Creating an account or
                updating its permissions immediately pushes a signed JWT to the
                resolver via <code>$SYS.REQ.CLAIMS.UPDATE</code>. Requires{" "}
                <code>OPERATOR_NKEY</code> to be configured on the API.
            </p>
            {(capabilities === null ||
                capabilities.accountDelete === false) && (
                <p className="notice">
                    Account delete is currently disabled by the NATS resolver.
                    Enable <code>allow_delete: true</code> in the resolver
                    configuration to remove accounts permanently.
                </p>
            )}
            {error && <p className="error">{error}</p>}
            <section className="panel">
                <OperatorList
                    operators={operators.map((name) => ({ name }))}
                    selectedName={selectedOperator}
                    onSelect={handleSelectOperator}
                />
                {selectedOperator && (
                    <div style={{ marginTop: "1rem" }}>
                        <AccountForm
                            operator={selectedOperator}
                            onCreated={handleCreated}
                        />
                    </div>
                )}
            </section>
            <div className="two-col">
                <section className="panel">
                    <h3>Accounts</h3>
                    <ul className="list">
                        {selectedAccounts.length === 0 && (
                            <li className="muted">
                                No accounts for this operator.
                            </li>
                        )}
                        {selectedAccounts.map((acc) => {
                            const k = `${acc.operator}/${acc.publicKey}`;
                            return (
                                <li
                                    key={k}
                                    className="list-row"
                                    style={{
                                        background:
                                            selectedAccountKey === k
                                                ? "rgba(59,130,246,0.12)"
                                                : undefined,
                                        borderRadius: "6px",
                                        cursor: "pointer",
                                        paddingInline: "0.4rem",
                                    }}
                                    onClick={() => setSelectedAccountKey(k)}
                                >
                                    <span>{acc.name}</span>
                                    {acc.jsEnabled && (
                                        <span className="badge ok">JS</span>
                                    )}
                                    {acc.isSystem && (
                                        <span className="badge">SYS</span>
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                </section>
                <section className="panel">
                    {selectedAccount2 ? (
                        <AccountList
                            accounts={[selectedAccount2]}
                            canDeleteAccounts={
                                capabilities !== null &&
                                capabilities.accountDelete !== false
                            }
                            onDeleteAccount={handleDeleteAccount}
                            onAddPublishAllow={handleAddPublishAllow}
                            onRemovePublishAllow={handleRemovePublishAllow}
                            onAddSubscribeAllow={handleAddSubscribeAllow}
                            onRemoveSubscribeAllow={handleRemoveSubscribeAllow}
                            onCreateUser={handleCreateUser}
                            onDeleteUser={handleDeleteUser}
                            onAddUserPublishAllow={handleAddUserPublishAllow}
                            onRemoveUserPublishAllow={
                                handleRemoveUserPublishAllow
                            }
                            onAddUserPublishDeny={handleAddUserPublishDeny}
                            onRemoveUserPublishDeny={handleRemoveUserPublishDeny}
                            onGetUserCreds={handleGetUserCreds}
                            onGetAccountJWT={handleGetAccountJWT}
                            onToggleJetStream={handleToggleJetStream}
                            onGrantJSConsumerApiAccess={
                                handleGrantJSConsumerApiAccess
                            }
                            onRevokeJSConsumerApiAccess={
                                handleRevokeJSConsumerApiAccess
                            }
                        />
                    ) : (
                        <p className="muted">
                            Select an account to view details.
                        </p>
                    )}
                </section>
            </div>
        </div>
    );
}
