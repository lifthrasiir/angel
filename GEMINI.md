- **Project:** Angel - A personalized coding agent using Go and React/TypeScript.
- **Goal:** Create a simple, single-user web version of `@google/gemini-cli`.
- **Language:** Code and comments should be in English. This very file should also be in English. Respond to the user in their requested language.
- **Terminology:** The terms "agent" or "angel" refer to the LLM model.
- **Development:** Features may require modifications to both the Go and TypeScript code. Try to refactor any significant duplications or similar structures.
- **Build:** Always run the minimal necessary build command: `npm run build-frontend` (frontend-only), `npm run build-backend` (backend-only), or `npm run build` (both). Never `npm start` (user responsibility). Run without prompt.
- **Dependency:** Minimize. Clearly explain why any new dependency is required.
- **Comments:** Comments are strictly for future maintainers, explaining *why* complex or non-obvious code exists, not *what* it does. Avoid any comments that describe new features, temporary changes, or are only relevant during code generation (e.g., "new endpoint", "add foo"). Ensure comments provide essential context beyond the code itself.
- **Modern Practices:** Prioritize current, idiomatic patterns and best practices for relevant frameworks/languages, ensuring up-to-date, performant, and maintainable solutions, unless contradicted by project conventions.
- **Responsiveness:** Always prioritize and act on the user's *latest* input. If a new instruction arrives during an ongoing task, immediately halt and address the new instruction; do not assume continuation.

# Specific instructions

- Never, ever remove the intentionally hard-coded GoogleOauthConfig!!!!
- You do NOT need to export anything in the same package!!!!
- `gemini_types.go` is for the official Gemini API types, and nothing else.
- The streaming protocol intentionally avoids `event:`.
- Feel free to use `git checkout` to roll your modification back.
- When using `replace` or `write_file`, pay close attention to newlines and whitespace. These tools demand exact literal matches.
  - **`replace`:** `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines (`\n` or `\r\n`). Read sufficient context (via `read_file` with `limit`, or `type`/`cat`) to form accurate `old_string` and respect file's newline convention.
  - **Complex Changes:** For complex modifications prone to `replace` errors, prefer `write_file` (read file, modify in memory, then overwrite).
