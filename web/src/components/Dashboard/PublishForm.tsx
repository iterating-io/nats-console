import { useState } from "react";
import { useEffect } from "react";
import type { SubmitEvent } from "react";
import { useAuth } from "../../context/AuthContext";

type Props = {
    apiBase: string;
    onEvent: (line: string) => void;
};

type UserRef = {
    name: string;
    account: string;
    operator: string;
    publicKey: string;
    publishAllow: string[];
};

export default function PublishForm({ apiBase, onEvent }: Props) {
    const { token, role } = useAuth();
    const [subject, setSubject] = useState("console.test");
    const [message, setMessage] = useState("hello from nats-console");
    const [users, setUsers] = useState<UserRef[]>([]);
    const [selectedUserKey, setSelectedUserKey] = useState("");
    const [error, setError] = useState("");
    const [info, setInfo] = useState("");

    const canPublish = role === "admin" || role === "operator";

    useEffect(() => {
        if (!token) {
            setUsers([]);
            setSelectedUserKey("");
            return;
        }
        const loadUsers = async () => {
            const res = await fetch(`${apiBase}/api/v1/users`, {
                headers: { Authorization: `Bearer ${token}` },
            });
            if (!res.ok) {
                setError("Failed to load users for publish test.");
                return;
            }
            const data = (await res.json()) as { users: UserRef[] };
            const list = data.users ?? [];
            setUsers(list);
            if (list.length > 0) {
                setSelectedUserKey(
                    `${list[0].operator}/${list[0].account}/${list[0].name}`,
                );
            }
        };
        loadUsers().catch(() =>
            setError("Failed to load users for publish test."),
        );
    }, [apiBase, token]);

    const onPublish = async (event: SubmitEvent<HTMLFormElement>) => {
        event.preventDefault();
        setError("");
        setInfo("");

        const selectedUser = users.find(
            (u) => `${u.operator}/${u.account}/${u.name}` === selectedUserKey,
        );
        if (!selectedUser) {
            setError("Select a user first.");
            return;
        }

        const res = await fetch(`${apiBase}/api/v1/publish/as-user`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                Authorization: `Bearer ${token}`,
            },
            body: JSON.stringify({
                operator: selectedUser.operator,
                account: selectedUser.account,
                user: selectedUser.name,
                subject,
                message,
            }),
        });

        if (!res.ok) {
            const data = (await res
                .json()
                .catch(() => ({ error: "publish failed" }))) as {
                error?: string;
            };
            setError(`Publish failed: ${data.error ?? "unknown error"}`);
            return;
        }

        onEvent(
            `published ${subject} as ${selectedUser.name} at ${new Date().toLocaleTimeString()}`,
        );
        setInfo(`Message sent as ${selectedUser.name} to ${subject}.`);
    };

    return (
        <section className="panel">
            <h2>Publish Test Message</h2>
            <form onSubmit={onPublish} className="stack">
                <select
                    value={selectedUserKey}
                    onChange={(e) => setSelectedUserKey(e.target.value)}
                    className="select-input"
                >
                    {users.length === 0 && (
                        <option value="">No users available</option>
                    )}
                    {users.map((u) => {
                        const key = `${u.operator}/${u.account}/${u.name}`;
                        return (
                            <option key={key} value={key}>
                                {u.operator}/{u.account}/{u.name}
                            </option>
                        );
                    })}
                </select>
                <input
                    value={subject}
                    onChange={(e) => setSubject(e.target.value)}
                    placeholder="subject"
                />
                <textarea
                    value={message}
                    onChange={(e) => setMessage(e.target.value)}
                    placeholder="message"
                    rows={5}
                />
                <button
                    type="submit"
                    disabled={!token || !canPublish || users.length === 0}
                >
                    Publish
                </button>
            </form>
            {!canPublish && token && (
                <p className="muted">Viewer role cannot publish messages.</p>
            )}
            {users.length === 0 && token && canPublish && (
                <p className="muted">Create a user in Accounts page first.</p>
            )}
            {error && <p className="error">{error}</p>}
            {info && <p className="info">{info}</p>}
        </section>
    );
}
