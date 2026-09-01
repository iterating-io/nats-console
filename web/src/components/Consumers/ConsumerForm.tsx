import { useState } from "react";
import type { SubmitEvent } from "react";

type Props = {
    streams: string[];
    onCreated: (stream: string, name: string, filterSubject: string, deliverPolicy: "all" | "new", ackWait: string, maxDeliver: number, maxAckPending: number) => void;
    fixedStream?: string;
};

type InfoTipProps = {
    description: string;
    items: string[];
};

export function InfoTip({ description, items }: InfoTipProps) {
    const accessibleText = [description, ...items].join(" ");
    return (
        <span className="info-tip" tabIndex={0} aria-label={accessibleText}>
            i
            <span role="tooltip" className="info-tip-content">
                <span className="info-tip-description">{description}</span>
                <ul>
                    {items.map((item) => <li key={item}>{item}</li>)}
                </ul>
            </span>
        </span>
    );
}

export default function ConsumerForm({
    streams,
    onCreated,
    fixedStream,
}: Props) {
    const [stream, setStream] = useState(fixedStream || "");
    const [name, setName] = useState(fixedStream ? `${fixedStream}_CONSUMER` : "");
    const [filterSubject, setFilterSubject] = useState("");
    const [deliverPolicy, setDeliverPolicy] = useState<"all" | "new">("all");
    const [ackWait, setAckWait] = useState("30s");
    const [maxDeliver, setMaxDeliver] = useState("-1");
    const [maxAckPending, setMaxAckPending] = useState("1000");
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
        onCreated(selectedStream, name.trim(), filterSubject.trim(), deliverPolicy, ackWait.trim(), parsedMaxDeliver, parsedMaxAckPending);
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
                <label className="consumer-field">
                    <span className="field-label">Consumer name <InfoTip description="Identifies the durable Consumer and its saved position." items={["Default: <STREAM>_CONSUMER", "Shared by service instances using the same Consumer", "Cannot be renamed after creation"]} /></span>
                    <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Consumer name" />
                </label>
                <label className="consumer-field">
                    <span className="field-label">Filter subject <InfoTip description="Limits which Stream messages this Consumer receives." items={["Empty: receive every Stream subject", "A subject value: receive matching messages only", "Supports NATS wildcards * and >"]} /></span>
                    <input value={filterSubject} onChange={(e) => setFilterSubject(e.target.value)} placeholder="Optional" />
                </label>
                <fieldset className="consumer-options">
                    <legend>Start position <InfoTip description="Determines where a newly created Consumer starts reading." items={["Stored messages: start with the oldest retained message", "New messages only: start after Consumer creation", "Cannot be changed after creation"]} /></legend>
                    <label><input type="radio" name="deliverPolicy" value="all" checked={deliverPolicy === "all"} onChange={() => setDeliverPolicy("all")} /> Stored messages <InfoTip description="Processes the Stream backlog before new messages." items={["NATS policy: DeliverAll", "Starts at the oldest retained message", "Recommended when no existing work may be skipped"]} /></label>
                    <label><input type="radio" name="deliverPolicy" value="new" checked={deliverPolicy === "new"} onChange={() => setDeliverPolicy("new")} /> New messages only <InfoTip description="Ignores the existing backlog and waits for new messages." items={["NATS policy: DeliverNew", "Existing stored messages are not delivered", "Useful for real-time processing without history"]} /></label>
                </fieldset>
                <p className="muted consumer-defaults">Pull Consumer · Explicit ACK · Instant replay <InfoTip description="Console-managed immutable defaults for a reliable worker Consumer." items={["Pull: workers request messages at their own pace", "Explicit ACK: each successfully processed message must be acknowledged", "Instant replay: stored messages are delivered without their original time gaps"]} /></p>
                <details className="consumer-advanced">
                    <summary>Advanced settings</summary>
                    <div className="consumer-advanced-grid">
                        <label className="consumer-field"><span className="field-label">ACK wait <InfoTip description="Time allowed to process a message before redelivery." items={["Default: 30s", "Increase for long-running work", "Can be updated later"]} /></span><input value={ackWait} onChange={(e) => setAckWait(e.target.value)} placeholder="30s" /></label>
                        <label className="consumer-field"><span className="field-label">Max deliveries <InfoTip description="Maximum delivery attempts for one message." items={["-1: unlimited retries", "Positive number: stop after that many attempts", "Can be updated later"]} /></span><input type="number" value={maxDeliver} onChange={(e) => setMaxDeliver(e.target.value)} /></label>
                        <label className="consumer-field"><span className="field-label">Max ACK pending <InfoTip description="Limits messages delivered but not yet acknowledged." items={["Default: 1000", "Lower values reduce worker pressure", "Can be updated later"]} /></span><input type="number" min="1" value={maxAckPending} onChange={(e) => setMaxAckPending(e.target.value)} /></label>
                    </div>
                </details>
                <button type="submit">Create</button>
                {error && <p className="error">{error}</p>}
            </form>
        </div>
    );
}
