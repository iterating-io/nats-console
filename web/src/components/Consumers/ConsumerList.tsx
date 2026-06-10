type Consumer = {
    name: string;
    filterSubject: string;
};

type Props = {
    consumers: Consumer[];
    onDelete: (name: string) => void;
};

export default function ConsumerList({ consumers, onDelete }: Props) {
    return (
        <div>
            <h3>Consumers</h3>
            <ul className="list">
                {consumers.length === 0 && (
                    <li className="muted">No consumers found.</li>
                )}
                {consumers.map((c) => (
                    <li key={c.name} className="list-row">
                        <span>
                            {c.name}
                            {c.filterSubject && (
                                <span className="muted">
                                    {" "}
                                    — {c.filterSubject}
                                </span>
                            )}
                        </span>
                        <button
                            type="button"
                            className="delete-btn"
                            onClick={() => {
                                if (!window.confirm(`Delete consumer '${c.name}'?`)) return;
                                onDelete(c.name);
                            }}
                        >
                            Delete
                        </button>
                    </li>
                ))}
            </ul>
        </div>
    );
}
