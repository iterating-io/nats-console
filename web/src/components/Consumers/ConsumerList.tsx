import { useState } from "react";
import { InfoTip } from "./ConsumerForm";
import type { SubmitEvent } from "react";

export type Consumer = {
    name: string;
    filterSubject: string;
    deliverPolicy: string;
    ackPolicy: string;
    ackWait: string;
    maxDeliver: number;
    maxAckPending: number;
    type: "pull" | "push";
};

type UpdateSettings = Pick<Consumer, "filterSubject" | "ackWait" | "maxDeliver" | "maxAckPending">;

type Props = {
    consumers: Consumer[];
    onDelete: (name: string) => void;
    onUpdate: (name: string, settings: UpdateSettings) => Promise<boolean>;
};

export default function ConsumerList({ consumers, onDelete, onUpdate }: Props) {
    const [editing, setEditing] = useState<string | null>(null);

    return (
        <div>
            <h3>Consumers</h3>
            <ul className="list consumer-list">
                {consumers.length === 0 && (
                    <li className="muted">No consumers found.</li>
                )}
                {consumers.map((consumer) => (
                    <ConsumerItem
                        key={consumer.name}
                        consumer={consumer}
                        editing={editing === consumer.name}
                        onEdit={() => setEditing(consumer.name)}
                        onCancel={() => setEditing(null)}
                        onDelete={onDelete}
                        onUpdate={async (settings) => {
                            const updated = await onUpdate(consumer.name, settings);
                            if (updated) setEditing(null);
                        }}
                    />
                ))}
            </ul>
        </div>
    );
}

type ConsumerItemProps = {
    consumer: Consumer;
    editing: boolean;
    onEdit: () => void;
    onCancel: () => void;
    onDelete: (name: string) => void;
    onUpdate: (settings: UpdateSettings) => Promise<void>;
};

function ConsumerItem({ consumer, editing, onEdit, onCancel, onDelete, onUpdate }: ConsumerItemProps) {
    const [filterSubject, setFilterSubject] = useState(consumer.filterSubject);
    const [ackWait, setAckWait] = useState(consumer.ackWait);
    const [maxDeliver, setMaxDeliver] = useState(String(consumer.maxDeliver));
    const [maxAckPending, setMaxAckPending] = useState(String(consumer.maxAckPending));
    const [error, setError] = useState("");

    const submit = async (event: SubmitEvent<HTMLFormElement>) => {
        event.preventDefault();
        setError("");
        const parsedMaxDeliver = Number(maxDeliver);
        const parsedMaxAckPending = Number(maxAckPending);
        if (!Number.isInteger(parsedMaxDeliver) || parsedMaxDeliver === 0 || parsedMaxDeliver < -1) {
            setError("Max deliveries must be -1 or a positive whole number.");
            return;
        }
        if (!Number.isInteger(parsedMaxAckPending) || parsedMaxAckPending < 1) {
            setError("Max ACK pending must be a positive whole number.");
            return;
        }
        await onUpdate({
            filterSubject: filterSubject.trim(),
            ackWait: ackWait.trim(),
            maxDeliver: parsedMaxDeliver,
            maxAckPending: parsedMaxAckPending,
        });
    };

    return (
        <li className="list-row consumer-card">
            <div className="consumer-card-header">
                <div>
                    <strong>{consumer.name}</strong>
                    <div className="consumer-summary">
                        <span className="badge">{consumer.type}</span>
                        <span className="badge">{consumer.ackPolicy} ACK</span>
                        <span className="badge">start: {consumer.deliverPolicy}</span>
                    </div>
                </div>
                <div className="consumer-actions">
                    <button type="button" className="secondary-btn" onClick={onEdit} disabled={editing}>Edit settings</button>
                    <button
                        type="button"
                        className="delete-btn"
                        onClick={() => {
                            if (window.confirm(`Delete consumer '${consumer.name}'?`)) onDelete(consumer.name);
                        }}
                    >
                        Delete
                    </button>
                </div>
            </div>

            {!editing && (
                <dl className="consumer-settings">
                    <div><dt>Filter</dt><dd>{consumer.filterSubject || "All Stream subjects"}</dd></div>
                    <div><dt>ACK wait</dt><dd>{consumer.ackWait}</dd></div>
                    <div><dt>Max deliveries</dt><dd>{consumer.maxDeliver === -1 ? "Unlimited" : consumer.maxDeliver}</dd></div>
                    <div><dt>Max ACK pending</dt><dd>{consumer.maxAckPending}</dd></div>
                </dl>
            )}

            {editing && (
                <form className="consumer-edit-form" onSubmit={submit}>
                    <p className="notice">Consumer type, ACK policy, and start position are fixed. Create a new Consumer to change them.</p>
                    <div className="consumer-advanced-grid">
                        <label className="consumer-field"><span className="field-label">Filter subject <InfoTip description="Limits which Stream subjects this Consumer receives." items={["Empty receives every Stream subject", "Supports NATS wildcards * and >", "Can be updated later"]} /></span><input value={filterSubject} onChange={(event) => setFilterSubject(event.target.value)} placeholder="All Stream subjects" /></label>
                        <label className="consumer-field"><span className="field-label">ACK wait <InfoTip description="Time allowed before an unacknowledged message is redelivered." items={["Use a duration such as 30s or 2m", "Increase for long-running work", "Can be updated later"]} /></span><input value={ackWait} onChange={(event) => setAckWait(event.target.value)} /></label>
                        <label className="consumer-field"><span className="field-label">Max deliveries <InfoTip description="Maximum delivery attempts for one message." items={["-1 means unlimited retries", "A positive number limits attempts", "Can be updated later"]} /></span><input type="number" value={maxDeliver} onChange={(event) => setMaxDeliver(event.target.value)} /></label>
                        <label className="consumer-field"><span className="field-label">Max ACK pending <InfoTip description="Maximum messages delivered but not yet acknowledged." items={["Provides backpressure for workers", "Lower values reduce concurrent load", "Can be updated later"]} /></span><input type="number" min="1" value={maxAckPending} onChange={(event) => setMaxAckPending(event.target.value)} /></label>
                    </div>
                    {error && <p className="error">{error}</p>}
                    <div className="consumer-actions">
                        <button type="button" className="secondary-btn" onClick={onCancel}>Cancel</button>
                        <button type="submit">Update settings</button>
                    </div>
                </form>
            )}
        </li>
    );
}
