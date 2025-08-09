# Key Concepts and Cautions for Agent Working on this Codebase

- **Project:** Angel - A personalized coding agent using Go and React/TypeScript.
- **Goal:** Create a simple, single-user web version of `@google/gemini-cli`.

## Project Features

- **Leverages Gemini Code Assist Free Tier**: The application integrates with the Gemini Code Assist API for LLM functionalities, utilizing the free tier. Authentication is handled via Google OAuth.
- **Multi-session and Workspaces**: Users can create and manage multiple chat sessions, which can be organized into distinct workspaces.
- **Branching**: Users can create new conversation branches from existing user messages and switch between different branches within a session, allowing for exploration of alternative conversation paths.
  - **Creating a Branch from a Message**: When creating a new branch from a specific `updatedMessageId` (via `/api/chat/{sessionId}/branch` POST), the new branch's conversation path begins from the *parent* of the `updatedMessageId`. The `updatedMessageId` itself is a reference point, and its parent's `chosen_next_id` is updated to point to the first message of the new branch, making it the active path.
- **Configurable System Prompt per Session**: Each chat session allows for a custom system prompt. These prompts support Go templating for dynamic generation, with a preview feature available in the UI.
- **Automatic Session Name Inference**: The LLM can infer and update session names based on conversation content.
- **Thought Display**: The LLM's internal thought processes are streamed as "thought" messages to the user interface. These thoughts are grouped and can be expanded/collapsed for detailed viewing.
- **File Upload and Attachment**: Users can attach files to their messages. Attached files are Base64 encoded, stored with the message, and support preview and download functionalities.
- **Tool Usage**: The LLM can invoke both built-in tools (e.g., `list_directory`, `read_file`) and external tools via the Model Context Protocol (MCP).

## Go Backend

- **Authentication (`auth.go`, `main.go`, `gemini.go`, `db.go`)**:
  - Defines the `Auth` interface, abstracting authentication functionalities for pluggable authentication methods.
  - Authentication method is selected based on environment variables (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_CLOUD_PROJECT`/`GOOGLE_CLOUD_LOCATION`) or Google OAuth login.
  - The `GoogleOauthConfig` is intentionally hard-coded to match `gemini-cli` and **must never be removed**.
  - Handles OAuth2 login and callbacks, including CSRF protection using `oauthStates`.
  - Authentication tokens are stored and loaded from an SQLite database (`angel.db`, `oauth_tokens` table).
  - Authentication tokens are automatically saved to the database after being obtained or refreshed via `tokenSaverSource`.
  - Manages user email information and login status.
  - The `InitCurrentProvider` function handles the initialization of the LLM client, including proactive token refresh for OAuth and automatic acquisition of the `ProjectID` via `LoadCodeAssist` and `OnboardUser` calls.
  - The `Validate` method ensures that the LLM client is properly initialized and a `ProjectID` is set before processing API requests, attempting re-initialization if necessary.
- **Database (`db.go`)**:
  - Uses SQLite (`angel.db`) for persistent storage.
  - Key tables include `sessions`, `messages`, `oauth_tokens`, `workspaces`, `mcp_configs`, and `branches`.
  - The `messages` table utilizes `parent_message_id` and `chosen_next_id` to define conversation threads and active branches, and `branch_id` to associate messages with specific branches.
  - Supports storing file attachments (`attachments` field) and token counts (`cumul_token_count`) within messages.
  - Provides CRUD operations for workspaces, sessions, messages, and branches.
- **Gemini API Interaction (`gemini.go`, `gemini_types.go`, `llm.go`)**:
  - Defines the `LLMProvider` interface as an abstraction layer for interacting with various Large Language Models, enabling extensibility.
  - The `CodeAssistClient` facilitates communication with the Gemini API.
  - Supports both streaming (`streamGenerateContent`) and one-shot (`GenerateContentOneShot`) responses.
  - Includes functionalities for token counting (`CountTokens`), loading code assist features (`LoadCodeAssist`), and user onboarding (`OnboardUser`).
  - Interacts with internal Google API endpoints for `streamGenerateContent`, `countTokens`, `loadCodeAssist`, and `onboardUser`.
  - The `LoadCodeAssist` function handles free tier eligibility checks and displays privacy notices to the user.
  - **Important**: The streaming protocol is a custom JSON stream format, not standard Server-Sent Events (SSE), and intentionally avoids the `event:` prefix.
  - `gemini_types.go` is strictly reserved for official Gemini API types and should not contain any other definitions.
- **Model Context Protocol (MCP) Management (`mcp.go`)**:
  - The `MCPManager` handles connections to multiple MCP servers.
  - Resolves naming conflicts between built-in tools and MCP server tools by prefixing MCP tool names (e.g., `mcpName__toolName`).
  - Dispatches tool calls to the appropriate MCP server via `DispatchToolCall`.
  - Utilizes Server-Sent Events (SSE) for MCP connections.
- **Server-Sent Events (SSE) Implementation (`sse.go`)**:
  - The `sseWriter` wraps `http.ResponseWriter` and `http.Flusher` to stream real-time updates to clients.
  - Defines various event types (e.g., `EventSessionID`, `EventThought`, `EventModelMessage`, `EventFunctionCall`, `EventFunctionReply`) to categorize messages sent to the frontend.
  - Manages client connection lifecycle and supports broadcasting events to active SSE clients.
- **Tool Definition and Management (`tools.go`)**:
  - Defines tools available to the LLM (e.g., `list_directory`, `read_file`), currently with mock implementations.
  - Converts JSON schemas to Gemini API-compatible schemas.
  - `CallToolFunction` dispatches calls to either local tools or MCP tools.
- **Prompt Management (`prompts.go`, `prompts_builtin.go`)**:
  - Uses Go templating for evaluating prompts (`EvaluatePrompt`).
  - `prompts_builtin.go` defines built-in prompt templates for core agent behavior, conversation summarization, and session name inference.
- **Project Entry Point (`main.go`)**:
  - Serves as the main entry point for the Go application.
  - Sets up the HTTP server and defines API routes using the `gorilla/mux` router.
  - Serves static frontend files, either from the filesystem during development or embedded files in production.
  - Initializes global state and services, including `GlobalGeminiState`, database (`InitDB`), Model Context Protocol manager (`InitMCPManager`), and authentication (`InitAuth`).
  - Includes common helper functions for JSON request/response handling, SSE header setup, and authentication validation.
  - Exposes `/api/models` endpoint to list available LLM models and their token limits.

## React/TypeScript Frontend

- **Core Technologies**: Built with React, TypeScript, and Vite.
- **UI Components**:
  - **`ChatArea.tsx`**: The central component for the chat interface, managing message display, system prompt editing, file attachment previews, and the chat input field.
  - **`ChatLayout.tsx`**: Defines the overall application layout, handling authentication rendering, integrating the sidebar and chat area, and displaying toast messages.
  - **`SystemPromptEditor.tsx`**: Provides the UI for editing and previewing system prompts. It supports selecting prompt types, custom editing, evaluating Go templates via the backend API (`/api/evaluatePrompt`), expanding/collapsing content, and a read-only mode for existing sessions.
  - **`InitialState`**: This struct is used to send the initial state of a chat session to the frontend. It includes the `SessionId` (which corresponds to the `sessions.id` in the database and is used for URL updates), the `History` of messages, the `SystemPrompt` for the session, the `WorkspaceID` it belongs to, and the `BranchID` of the currently active branch within that session. The `SessionId` is a string generated by `generateID()` and is used consistently across the frontend for routing and identification.
  - **Global Error Handling**: Implements a global `window.onerror` handler to catch uncaught JavaScript errors and display them using the `ToastMessage` component for improved user feedback.
  - **Model Selection**: Allows users to select different LLM models for their chat sessions, leveraging the `/api/models` endpoint to fetch available models and their token limits.
  - **MCP Settings**: Provides a user interface (`MCPSettings.tsx`) for managing Model Context Protocol (MCP) server configurations, including viewing connection status, available tools, and adding/deleting configurations.
- **State Management**: Utilizes Jotai for global state management across the chat application.
- **Custom Hooks**:
  - **`useChatSession.ts`**: A central hub hook that encapsulates all chat session-related state and logic. It integrates `useChat`, `useMessageSending`, `useSessionInitialization`, `useWorkspaceAndSessions`, and manages model selection, ensuring the UI reflects the currently used model. Its connection to the backend is indirect, facilitated by the integrated hooks.
  - **`useMessageSending.ts`**: Encapsulates the logic for sending messages and streaming subsequent responses.
    - **Backend Connection**: Sends user messages (with attachments) via POST requests to the `/api/chat` endpoint (for new sessions) or `/api/chat/{sessionId}` (for existing sessions).
    - **Streaming Processing**: Processes streaming responses from the backend (using SSE event types defined in `sse.go`) to update the UI in real-time.
    - **Cancellation**: Sends a DELETE request to `/api/chat/{sessionId}/call` to cancel ongoing streaming, which is handled by the `cancelCall` function in `call_manager.go` on the backend.
  - **`useSessionInitialization.ts`**: Encapsulates the logic for loading existing chat sessions and possibly streaming the ongoing call.
    - **Backend Connection**: Establishes an SSE connection to the `/api/chat/{sessionId}` endpoint via the `loadSession` function to receive session history and real-time updates.
    - **Authentication**: Fetches user information from the `/api/userinfo` endpoint via `fetchUserInfo` to verify login status.
  - **`useWorkspaceAndSessions.ts`**: Encapsulates the logic for fetching workspace and session data.
    - **Backend Connection**: Fetches workspace and session lists from the `/api/chat` (sessions list) endpoint via the `fetchSessions` function.

## General Project Concepts and Cautions

- **Language**: Code and comments are in English. User responses should be in the requested language (currently Korean).
- **Terminology**: The terms "agent" or "angel" refer to the LLM model.
- **Development**: Features may require modifications to both Go and TypeScript code. Aim to refactor significant duplications or similar structures.
- **Build:** Always run the minimal necessary build command: `npm run build-frontend` (frontend-only), `npm run build-backend` (backend-only), or `npm run build` (both). Never `npm start` (user responsibility). Run without prompt.
- **Tests:** `npm run test` (backend-only).
- **Dependency**: Minimize new dependencies. Clearly explain why any new dependency is required.
- **Comments**: Comments are strictly for future maintainers, explaining *why* complex or non-obvious code exists, not *what* it does. Avoid comments that describe new features, temporary changes, or are only relevant during code generation.
- **Modern Practices:** Prioritize current, idiomatic patterns and best practices for relevant frameworks/languages, ensuring up-to-date, performant, and maintainable solutions, unless contradicted by project conventions.
- **File Operations (`replace`, `write_file`)**:
  - `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines.
  - For complex modifications, prefer `write_file` (read file, modify in memory, then overwrite).
  - **Caution for Agents**: When providing string literals containing newlines to `replace` or `write_file` tools, ensure that newlines are *double-escaped* (e.g., `\n` becomes `\\n`, `\r\n` becomes `\\r\\n`). This is to prevent issues during JSON serialization of the tool arguments.
- **Responsiveness:** Always prioritize and act on the user's *latest* input. If a new instruction arrives during an ongoing task, you *must* immediately halt the current task and address the new instruction; do not assume continuation.

# Specific instructions

- Never, ever remove the intentionally hard-coded GoogleOauthConfig!!!!
- You do NOT need to export anything in the same package!!!!
- `gemini_types.go` is for the official Gemini API types, and nothing else.
- The streaming protocol intentionally avoids `event:`.
- Feel free to use `git checkout` to roll your modification back. But do not use any other git command unless requested.
- All workspace and session ID generated by `generateId()` should contain at least one capital letter. (CamelCase recommended for testing)
- When using `replace` or `write_file`, pay close attention to newlines and whitespace. These tools demand exact literal matches.
  - **`replace`:** `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines (`\n` or `\r\n`). Read sufficient context (via `read_file` with `limit`, or `type`/`cat`) to form accurate `old_string` and respect file's newline convention.
  - **Complex Changes:** For complex modifications prone to `replace` errors, prefer `write_file` (read file, modify in memory, then overwrite).
- **Go Error Handling:** When an error occurs, prefer re-declaring the variable using `:=` instead of `var` followed by `=` to avoid "declared and not used" errors and ensure proper variable scoping.
- **Addressing User Doubts:** If a user expresses doubt or questions a proposed solution, immediately pause the current task. Prioritize understanding the user's perspective and the reasoning behind their concerns. Engage in a dialogue to clarify their thoughts, address their specific points, and collaboratively arrive at a solution that aligns with their understanding and expectations. The goal is to ensure the user feels heard and confident in the approach.
