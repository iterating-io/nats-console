import type { Operator } from "../../types";

type Props = {
    operators: Operator[];
    selectedName?: string;
    onSelect?: (name: string) => void;
};

export default function OperatorList({
    operators,
    selectedName,
    onSelect,
}: Props) {
    return (
        <div>
            <h3>Operators</h3>
            <ul className="list">
                {operators.length === 0 && (
                    <li className="muted">No operators found.</li>
                )}
                {operators.map((op) => (
                    <li
                        key={op.name}
                        className="list-row"
                        style={{
                            background:
                                selectedName === op.name
                                    ? "rgba(59, 130, 246, 0.12)"
                                    : undefined,
                            borderRadius: "6px",
                            cursor: onSelect ? "pointer" : "default",
                            paddingInline: "0.4rem",
                        }}
                        onClick={() => onSelect?.(op.name)}
                    >
                        <span>{op.name}</span>
                    </li>
                ))}
            </ul>
        </div>
    );
}
