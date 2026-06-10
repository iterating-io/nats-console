type Stream = {
    name: string;
};

type Props = {
    streams: Stream[];
    onDelete: (name: string) => void;
    onSelect: (name: string) => void;
    selected: string;
};

export default function StreamList({
    streams,
    onDelete,
    onSelect,
    selected,
}: Props) {
    return (
        <div>
            <h3>Streams</h3>
            <ul className="list">
                {streams.length === 0 && (
                    <li className="muted">No streams found.</li>
                )}
                {streams.map((s) => (
                    <li
                        key={s.name}
                        className={`list-row${selected === s.name ? " list-row--active" : ""}`}
                    >
                        <button
                            type="button"
                            className="list-name-btn"
                            aria-pressed={selected === s.name}
                            onClick={() => onSelect(s.name)}
                        >
                            {s.name}
                            {selected === s.name && (
                                <span className="selected-badge">Selected</span>
                            )}
                        </button>
                        <button
                            type="button"
                            className="delete-btn"
                            onClick={() => {
                                if (!window.confirm(`Delete stream '${s.name}'?`)) return;
                                onDelete(s.name);
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
