#---
name: "Agent Operating Guidelines"
description: "Guidelines for session records and agent behavior."
---

# Agent Operating Guidelines

1. **Session Record Management**
    - All work derived from user questions and requests must be recorded as session files in the `.github/sessions/` directory.
    - Each session file must have a unique name that includes a timestamp.
        - Example: `session-20240918-153000.md`

    - The session file must be updated with every response to reflect the current conversation flow and work progress.
    - Session files should not accumulate lengthy change histories; instead, they must always maintain a **clean, up-to-date final state**.
    - If decisions change during the conversation, only the final confirmed state should be reflected in the session document.

2. **Explain Before Making Changes**
    - Any action that may result in an actual change to the system, files, settings, or data must be clearly explained to the user before execution.
    - Read-only actions such as inspection, verification, or analysis may be performed without prior explanation.

3. **Definition of Session Completion**
    - A session is considered complete when all work performed and decisions made during that session have been consolidated and reflected in the relevant project documentation as the **final confirmed state**.
    - Session completion **always** includes creating a git commit. The commit must be created as the final step of every session-close workflow, without exception.
    - At the time of completion, no temporary notes, intermediate states, or unresolved items should remain in the session record.

4. **Session File Cleanup**
    - The commit must be created after final documentation has been updated and before the session files are deleted.
    - Once a session is completed, all session files created in `.github/sessions/` for that session must be deleted.
    - Before deletion, confirm that all necessary final content has been properly transferred to the project documentation.
    - **Project documentation locations:**
        - `docs/architecture.md` — system design, data models, API contracts, component structure, and key technical decisions
        - `docs/operations.md` — deployment, configuration, environment variables, startup procedures, and operational notes
        - `README.md` — project overview, features, and end-user usage instructions only (no internal design details)

5. **Operating Principles**
    - Keep all records **clear, concise, and structured**.
    - Prioritize the **current accurate state** over preserving step-by-step history.
    - Clearly distinguish between actions that require user approval and those that do not.
    - Maintain a clear separation between **temporary records (session files)** and **permanent records (project documentation)**.

6. **Web Frontend Structure Rules**
    - Always create a dedicated directory per component (e.g., `src/components/Streams/`, `src/components/Consumers/`).
    - Routing must be done at the page level (e.g., `src/pages/StreamsPage.tsx`).
    - Pages import and compose components; pages themselves do not contain inline component logic.
    - Components are organized by NATS element units:
        - `Operator` — operator-level components
        - `Account` — account-level components
        - `Streams` — stream-level components
        - `Consumers` — consumer-level components
        - Other NATS elements (e.g., `KeyValue`, `ObjectStore`) follow the same pattern.

7. **Human Language Rule**
    - **English is the only permitted language** across all outputs: source code, comments, variable names, UI strings, error messages, log messages, session records, and documentation.
    - Keep all language clear, concise, and human-readable.
    - Avoid technical jargon that hinders understanding for non-technical stakeholders.
