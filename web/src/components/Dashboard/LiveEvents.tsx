type Props = {
    events: string[];
    error: string;
};

export default function LiveEvents({ events, error }: Props) {
    return (
        <section className="panel wide">
            <h2>Live Events</h2>
            <pre className="eventLog">
                {events.length ? events.join("\n") : "Waiting events..."}
            </pre>
            {error && <p className="error">{error}</p>}
        </section>
    );
}
