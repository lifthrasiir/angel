- **Project:** Angel - A personalized coding agent using Go and React/TypeScript.
- **Goal:** Create a simple, single-user web version of `@google/gemini-cli`.
- **Language:** Code and comments should be in English. This very file should also be in English. Respond to the user in their requested language.
- **Terminology:** The terms "agent" or "angel" refer to the LLM model.
- **Development:** Features may require modifications to both the Go and TypeScript code. Try to refactor any significant duplications or similar structures.
- **Build:** `npm run build-frontend`, `npm run build-backend`, `npm run build` (both). Strive to run the minimal command required. Never run `npm start`, the user will. Otherwise run without prompt.
- **Dependency:** Minimize. Clearly explain why any new dependency is required.
- **Documentation:** Comments are for future me, do not say what is new. Conversely include as many contexts as needed besides from code itself.

# Specific instructions

- Never, ever remove the intentionally hard-coded GoogleOauthConfig!!!!
- You do NOT need to export anything in the same package!!!!
- `gemini_types.go` is for the official Gemini API types, and nothing else.
- The streaming protocol intentionally avoids `event:`.
- Feel free to use `git checkout` to roll your modification back.
- When using `replace` or `write_file`, pay close attention to newlines and whitespace. These tools demand exact literal matches.
  - **`replace`:** `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines (`\n` or `\r\n`). Read sufficient context (via `read_file` with `limit`, or `type`/`cat`) to form accurate `old_string` and respect file's newline convention.
  - **Complex Changes:** For complex modifications prone to `replace` errors, prefer `write_file` (read file, modify in memory, then overwrite).
