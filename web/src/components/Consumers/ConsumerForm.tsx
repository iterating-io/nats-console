import { useState } from "react";
import type { SubmitEvent } from "react";

type Props = {
    streams: string[];
    onCreated: (stream: string, name: string, filterSubject: string) => void;
    fixedStream?: string;
};

export default function ConsumerForm({
    streams,
    onCreated,
    fixedStream,
}: Props) {
    const [stream, setStream] = useState(fixedStream || "");
    const [name, setName] = useState("");
    const [filterSubject, setFilterSubject] = useState("");
    const [error, setError] = useState("");

    const onSubmit = (e: SubmitEvent<HTMLFormElement>) => {
        e.preventDefault();
        setError("");
        const selectedStream = fixedStream || stream;
        if (!selectedStream) {
            setError("Please select a stream.");
            return;
        }
        if (!name.trim()) {
            setError("Consumer name is required.");
            return;
        }
        onCreated(selectedStream, name.trim(), filterSubject.trim());
        setName("");
        setFilterSubject("");
        if (!fixedStream) {
            setStream("");
        }
    };

    return (
        <div>
            <h3>Create Consumer</h3>
            <form onSubmit={onSubmit} className="stack">
                {!fixedStream && (
                    <select
                        value={stream}
                        onChange={(e) => setStream(e.target.value)}
                        className="select-input"
                    >
                        <option value="">Select stream…</option>
                        {streams.map((s) => (
                            <option key={s} value={s}>
                                {s}
                            </option>
                        ))}
                    </select>
                )}
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Consumer name"
                />
                <input
                    value={filterSubject}
                    onChange={(e) => setFilterSubject(e.target.value)}
                    placeholder="Filter subject (optional)"
                />
                <button type="submit">Create</button>
                {error && <p className="error">{error}</p>}
            </form>
        </div>
    );
}
