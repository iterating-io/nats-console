import { useState } from "react";
import type { SubmitEvent } from "react";

type Props = {
    operator: string;
    onCreated: (
        name: string,
        publishAllow: string[],
        subscribeAllow: string[],
    ) => void;
};

export default function AccountForm({ operator, onCreated }: Props) {
    const [name, setName] = useState("");
    const [publishAllowInput, setPublishAllowInput] = useState("");
    const [subscribeAllowInput, setSubscribeAllowInput] = useState("");
    const [error, setError] = useState("");

    const parseSubjects = (raw: string): string[] => {
        const seen = new Set<string>();
        return raw
            .split(",")
            .map((item) => item.trim())
            .filter((item) => {
                if (!item || seen.has(item)) {
                    return false;
                }
                seen.add(item);
                return true;
            });
    };

    const onSubmit = (e: SubmitEvent<HTMLFormElement>) => {
        e.preventDefault();
        setError("");
        if (!operator) {
            setError("Please select an operator from the list.");
            return;
        }
        const trimmed = name.trim();
        if (!trimmed) {
            setError("Account name is required.");
            return;
        }
        onCreated(
            trimmed,
            parseSubjects(publishAllowInput),
            parseSubjects(subscribeAllowInput),
        );
        setName("");
        setPublishAllowInput("");
        setSubscribeAllowInput("");
    };

    const [open, setOpen] = useState(false);

    return (
        <div>
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                style={{
                    background: "none",
                    border: "none",
                    cursor: "pointer",
                    display: "flex",
                    alignItems: "center",
                    gap: "0.4rem",
                    padding: 0,
                    fontWeight: 600,
                    fontSize: "1rem",
                    color: "inherit",
                }}
            >
                <span
                    style={{
                        display: "inline-block",
                        transition: "transform 0.15s",
                        transform: open ? "rotate(90deg)" : "rotate(0deg)",
                        fontSize: "0.8em",
                    }}
                >
                    ▶
                </span>
                Create Account
            </button>
            {open && (
                <form
                    onSubmit={onSubmit}
                    className="stack"
                    style={{ marginTop: "0.75rem" }}
                >
                    <div className="muted">
                        Operator: {operator || "(not selected)"}
                    </div>
                    <input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder="Account name"
                    />
                    <input
                        value={publishAllowInput}
                        onChange={(e) => setPublishAllowInput(e.target.value)}
                        placeholder="Publish allow (comma-separated, optional)"
                    />
                    <input
                        value={subscribeAllowInput}
                        onChange={(e) => setSubscribeAllowInput(e.target.value)}
                        placeholder="Subscribe allow (comma-separated, optional)"
                    />
                    <button type="submit">Create</button>
                    {error && <p className="error">{error}</p>}
                </form>
            )}
        </div>
    );
}
