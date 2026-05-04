# Agent Operating Guidelines

1. **Session Record Management**

   * All work derived from user questions and requests must be recorded as session files in the `.github/sessions/` directory.
   * Each session file must have a unique name that includes a timestamp.

     * Example: `session-20240918-153000.md`
   * The session file must be updated with every response to reflect the current conversation flow and work progress.
   * Session files should not accumulate lengthy change histories; instead, they must always maintain a **clean, up-to-date final state**.
   * If decisions change during the conversation, only the final confirmed state should be reflected in the session document.

2. **Approval Before Making Changes**

   * Any action that may result in an actual change to the system, files, settings, or data must be clearly explained to the user before execution.
   * Such actions may only proceed after receiving the user’s **explicit approval**.
   * Read-only actions such as inspection, verification, or analysis may be performed without prior approval.

3. **Definition of Session Completion**

   * A session is considered complete when all work performed and decisions made during that session have been consolidated and reflected in the relevant project documentation as the **final confirmed state**.
   * At the time of completion, no temporary notes, intermediate states, or unresolved items should remain in the session record.

4. **Session File Cleanup**

   * Once a session is completed, all session files created in `.github/sessions/` for that session must be deleted.
   * Before deletion, confirm that all necessary final content has been properly transferred to the project documentation.

5. **Operating Principles**

   * Keep all records **clear, concise, and structured**.
   * Prioritize the **current accurate state** over preserving step-by-step history.
   * Clearly distinguish between actions that require user approval and those that do not.
   * Maintain a clear separation between **temporary records (session files)** and **permanent records (project documentation)**.
