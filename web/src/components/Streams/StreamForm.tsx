import { useState } from "react";
import type { SubmitEvent } from "react";
import type { Account } from "../../types";

type Props = {
    accounts: Account[];
    selectedAccountPublicKey: string;
    onSelectAccount: (accountPublicKey: string) => void;
    onCreated: (name: string, subjects: string[]) => void;
};

export default function StreamForm({
    accounts,
    selectedAccountPublicKey,
    onSelectAccount,
    onCreated,
}: Props) {
    const [name, setName] = useState("");
    const [subjects, setSubjects] = useState("");
    const [error, setError] = useState("");

    const onSubmit = (e: SubmitEvent<HTMLFormElement>) => {
        e.preventDefault();
        setError("");
        if (!selectedAccountPublicKey) {
            setError("Please select an account.");
            return;
        }
        const trimmedName = name.trim();
        if (!trimmedName) {
            setError("Stream name is required.");
            return;
        }
        const subjectList = subjects
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean);
        onCreated(trimmedName, subjectList);
        setName("");
        setSubjects("");
    };

    return (
        <div>
            <h3>Create Stream</h3>
            <form onSubmit={onSubmit} className="stack">
                <select
                    value={selectedAccountPublicKey}
                    onChange={(e) => onSelectAccount(e.target.value)}
                    className="select-input"
                >
                    <option value="">Select account…</option>
                    {accounts
                        .filter((acc) => !acc.isSystem)
                        .map((acc) => (
                            <option key={acc.publicKey} value={acc.publicKey}>
                                {acc.name} ({acc.operator})
                                {acc.jsEnabled ? "" : " - JS Disabled"}
                            </option>
                        ))}
                </select>
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Stream name"
                />
                <input
                    value={subjects}
                    onChange={(e) => setSubjects(e.target.value)}
                    placeholder="Subjects (comma-separated, optional)"
                />
                <button type="submit">Create</button>
                {error && <p className="error">{error}</p>}
            </form>
        </div>
    );
}
