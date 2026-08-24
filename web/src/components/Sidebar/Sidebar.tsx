import { NavLink } from "react-router-dom";

type NavItem = {
    label: string;
    path: string;
};

type NavGroup = {
    group: string;
    items: NavItem[];
};

const NAV: NavGroup[] = [
    {
        group: "Authentication",
        items: [{ label: "Operators / Accounts", path: "/dashboard/accounts" }],
    },
    {
        group: "JetStream",
        items: [
            { label: "Streams / Consumers", path: "/dashboard/streams" },
            { label: "Messages", path: "/dashboard/messages" },
        ],
    },
    {
        group: "Integration",
        items: [{ label: "AsyncAPI", path: "/dashboard/asyncapi" }],
    },
];

export default function Sidebar() {
    return (
        <aside className="sidebar">
            <div className="sidebar-logo">
                <p className="eyebrow">NATS SYSTEM</p>
                <span className="sidebar-title">NATS Console</span>
            </div>
            <nav className="sidebar-nav">
                {NAV.map((section) => (
                    <div key={section.group} className="sidebar-group">
                        <span className="sidebar-group-label">
                            {section.group}
                        </span>
                        {section.items.map((item) => (
                            <NavLink
                                key={item.path}
                                to={item.path}
                                className={({ isActive }) =>
                                    `sidebar-link${isActive ? " active" : ""}`
                                }
                            >
                                {item.label}
                            </NavLink>
                        ))}
                    </div>
                ))}
            </nav>
        </aside>
    );
}
