# Key Concepts and Cautions for Agent Working on this Codebase

- **Project:** Angel - A personalized coding agent using Go and React/TypeScript.
- **Goal:** Create a simple, single-user web version of `@google/gemini-cli`.
- **Default LLM Model:** `gemini-2.5-flash` (with support for `gemini-2.5-flash-lite` and `gemini-2.5-flash-image` variants)

## Project Features

- **Leverages Gemini Code Assist Free Tier**: The application integrates with the Gemini Code Assist API for LLM functionalities, utilizing the free tier. Authentication is handled via Google OAuth.
- **Multi-session and Workspaces**: Users can create and manage multiple chat sessions, which can be organized into distinct workspaces.
- **Branching**: Users can create new conversation branches from existing user messages and switch between different branches within a session, allowing for exploration of alternative conversation paths. This is managed by linking messages using `parent_message_id` and `chosen_next_id` in the `messages` table, and associating them with specific `branch_id`s. The `primary_branch_id` in the `sessions` table indicates the currently active conversation path.
- **Configurable System Prompt per Session**: Each chat session allows for a custom system prompt. These prompts support Go templating for dynamic generation using the `EvaluatePrompt` function, with a preview feature available in the UI.
- **Automatic Session Name Inference**: The LLM can infer and update session names based on conversation content. This process is handled by `inferAndSetSessionName`, which uses specific prompts (`GetSessionNameInferencePrompts`) to guide the LLM.
- **Thought Display**: The LLM's internal thought processes are streamed as "thought" messages to the user interface. These thoughts are grouped and can be expanded/collapsed for detailed viewing. The `Part` struct includes a `Thought` field, and these messages are broadcast via the `EventThought` SSE event.
- **File Upload and Attachment**: Users can attach files to their messages. Files are Base64 encoded during transmission, but stored as raw binary data in a refcounted blob storage system via the `blobs` table, and support preview and download functionalities via the `/api/blob/{blobHash}` endpoint. The blob system automatically manages reference counting to prevent data loss when files are shared across multiple messages.
- **Tool Usage**: The LLM can invoke both built-in tools (e.g., `list_directory`, `read_file`, `write_file`, `run_shell_command`, `write_todo`, `web_fetch`, `subagent`, `generate_image`) and external tools via the Model Context Protocol (MCP). The `web_fetch` tool can extract URLs, check for private IPs, convert GitHub blob URLs to raw URLs, and includes a fallback mechanism for direct fetching. The `write_file` tool returns a unified diff for verification. The `subagent` tool allows spawning specialized subagents with custom system prompts for specific tasks. The `generate_image` tool enables image creation and editing using subagents with image generation capabilities. Directories can be exposed or unexposed via /(un)expose commands, and their contents and per-directory directives (GEMINI.md etc.) are given as dynamic system prompts.
- **Chat History Compression**: The chat history can be compressed into a concise, structured XML snapshot by the user explicitly running the `/compress` command. This process is handled by the `CompressSession` function, which uses token thresholds to determine when to summarize and updates the message chain with the compressed content.

## Go Backend

- **Authentication (`auth.go`, `main.go`, `gemini.go`)**: Handles user authentication via Google OAuth, storing tokens in an SQLite database. The `tokenSaverSource` in `gemini.go` ensures tokens are saved upon acquisition or refresh. `main.go` sets up OAuth2 handlers.
- **Database (`db.go`, `db_chat.go`)**: Uses SQLite (`angel.db`) for persistent storage of sessions, messages, and other configurations. `db_chat.go` specifically handles chat-related database operations. The `messages` table utilizes `parent_message_id`, `chosen_next_id`, and `branch_id` to manage conversation threads and branching. The `blobs` table stores file data with automatic reference counting via triggers. Functions like `AddMessageToSession` and `UpdateMessageContent` are crucial for message management.
- **Gemini API Interaction (`gemini.go`, `gemini_types.go`, `llm.go`)**:
  - Defines the `LLMProvider` interface in `llm.go` for abstracting interactions with various Large Language Models, including methods like `SendMessageStream` (for streaming responses), `GenerateContentOneShot` (for single-shot responses), `CountTokens`, `MaxTokens`, `RelativeDisplayOrder`, and `DefaultGenerationParams`.
  - `SessionParams` in `llm.go` holds all parameters for an LLM chat session, including `Contents`, `ModelName`, `SystemPrompt`, `IncludeThoughts`, `GenerationParams` (e.g., Temperature, TopK, TopP), and `ToolConfig`.
  - `gemini_types.go` strictly defines official Gemini API types such as `Content`, `Part` (which includes `Thought`, `FunctionCall`, `FunctionResponse`), `Schema` (for tool definitions), `GenerationConfig`, and various metadata types (`URLContextMetadata`, `GroundingMetadata`).
  - The `CodeAssistClient` in `gemini.go` facilitates communication with the Gemini API, handling token counting, code assist loading, and user onboarding.
- **Model Context Protocol (MCP) Management (`mcp.go`)**: The `MCPManager` in `mcp.go` handles connections to multiple MCP servers. It resolves naming conflicts between built-in and MCP tools by prefixing MCP tool names (e.g., `mcpName__toolName`) and dispatches tool calls via `DispatchToolCall`.
- **Server-Sent Events (SSE) Implementation (`sse.go`)**: The `sseWriter` in `sse.go` streams real-time updates to clients. It defines various `EventType`s (e.g., `EventInitialState`, `EventThought`, `EventModelMessage`, `EventFunctionCall`, `EventFunctionResponse`, `EventComplete`, `EventSessionName`, `EventCumulTokenCount`, `EventError`) and supports broadcasting events to active SSE clients. The streaming protocol is a custom JSON stream format, intentionally avoiding the `event:` prefix.
- **Tool Definition and Management (`tools.go`, `tools_*.go`)**: `tools.go` defines the core framework for tool management, including `ToolDefinition` (which specifies tool name, description, parameters, and handler). Built-in tools like `list_directory`, `read_file`, `web_fetch` (implemented in `tools_webfetch.go`), `subagent`, and `generate_image` (implemented in `tools_subagent.go`) are managed here. `GetToolsForGemini` prepares tools for the Gemini API, and `CallToolFunction` in `tools.go` dispatches calls to either local or MCP tools.
- **Prompt Management (`prompts.go`, `prompts_builtin.go`)**: Uses Go templating for evaluating prompts via the `EvaluatePrompt` function in `prompts.go`. `prompts_builtin.go` defines built-in prompt templates for core agent behavior (`GetDefaultSystemPrompt`), conversation summarization (`GetCompressionPrompt`), and session name inference (`GetSessionNameInferencePrompts`).
- **Project Entry Point (`main.go`)**: Initializes global state and services, including the database, MCP manager, and authentication. It also sets up the main HTTP router (`InitRouter`) and serves static files and SPA routes.

## React/TypeScript Frontend

- **Core Technologies**: Built with React, TypeScript, and Vite.
- **Key Files**:
  - **`main.tsx`**: The application's entry point, responsible for rendering the main `ChatLayout` component.
  - **`api/models.ts`**: Handles API calls related to fetching available LLM models.
  - **`atoms/chatAtoms.ts`**: Defines Jotai atoms for global state management, including chat sessions, messages, and UI states.
  - **`types/chat.ts`**: Contains TypeScript interface definitions for chat-related data structures, such as messages, sessions, and attachments.
  - **`utils/`**: This directory contains various utility functions, including `fileHandler.ts` (for file attachments), `measurementUtils.ts` (for UI element measurements), `messageHandler.ts` (for message processing), `sessionManager.ts` (for session-related operations), `stringUtils.ts` (for string manipulations), and `userManager.ts` (for user-related data).
- **UI Components**:
  - **`ChatArea.tsx`**: The central component for the chat interface, managing message display, system prompt editing, file attachment previews, and the chat input field. Supports infinite scrolling and message editing capabilities.
  - **`ChatInput.tsx`**: Integrated within `ChatArea.tsx`, handles user message input and sending with context-aware behavior (Enter key behavior depends on context).
  - **`ChatLayout.tsx`**: Defines the overall application layout, handling authentication rendering, integrating the sidebar and chat area, and displaying toast messages.
  - **`SystemPromptEditor.tsx`**: Provides the UI for editing and previewing system prompts. It supports selecting prompt types, custom editing, evaluating Go templates via the backend API (`/api/evaluatePrompt`), expanding/collapsing content, and a read-only mode for existing sessions.
  - **`InitialState`**: This struct is used to send the initial state of a chat session to the frontend. It includes the `SessionId` (which corresponds to the `sessions.id` in the database and is used for URL updates), the `History` of messages, the `SystemPrompt` for the session, the `WorkspaceID` it belongs to, and the `PrimaryBranchID` of the currently active branch within that session. The `SessionId` is a string generated by `generateID()` and is used consistently across the frontend for routing and identification.
  - **`MCPSettings.tsx`**: Provides a user interface for managing Model Context Protocol (MCP) server configurations, including viewing connection status, available tools, and adding/deleting configurations.
  - **Message Editing**: Users can edit messages (including the first message in a session) with explicit edit buttons, replacing focus-based editing with clearer UI controls. Edited messages can be retried to regenerate responses.
  - **Branch Management**: Enhanced branch listing and switching capabilities with improved UI controls for managing conversation branches.
  - **Drag & Drop Support**: Proper drag & drop support for internal images and file attachments with improved file handling.
  - **Mobile Support**: Minimal mobile support for basic functionality on smaller screens.
  - **Syntax Highlighting**: Enhanced code display with proper syntax highlighting, including within unified diffs.
  - **Global Error Handling**: Implements a global `window.onerror` handler to catch uncaught JavaScript errors and display them using the `ToastMessage` component for improved user feedback.
  - **Model Selection**: Allows users to select different LLM models for their chat sessions, including support for `gemini-2.5-flash`, `gemini-2.5-flash-lite`, and `gemini-2.5-flash-image` variants, leveraging the `/api/models` endpoint to fetch available models and their token limits.
  - **Other UI Components**: Includes components for displaying individual messages (`ChatMessage.tsx`, `UserTextMessage.tsx`, `ModelTextMessage.tsx`, `FunctionCallMessage.tsx`, `FunctionResponseMessage.tsx`, `FunctionPairMessage.tsx`, `SystemMessage.tsx`, `CompressionMessage.tsx`, `EnvChangedMessage.tsx`), managing session and workspace lists (`SessionList.tsx`, `WorkspaceList.tsx`), rendering markdown (`MarkdownRenderer.tsx`), displaying thoughts (`ThoughtGroup.tsx`), and specialized tool components (`GenerateImage.tsx`, `BlobImage.tsx`).
- **State Management**: Utilizes Jotai for global state management across the chat application.
- **Custom Hooks**:
  - **`useChatSession.ts`**: A central hub hook that encapsulates all chat session-related state and logic. It integrates `useChat`, `useMessageSending`, `useSessionLoader`, and `useWorkspaceAndSessions`.
  - **`useMessageSending.ts`**: Encapsulates the logic for sending messages and streaming subsequent responses. 
    - **Backend Connection**: Sends user messages (with attachments) via POST requests to the `/api/chat` endpoint (for new sessions) or `/api/chat/{sessionId}` (for existing sessions).
    - **Streaming Processing**: Processes streaming responses from the backend (using SSE event types defined in `sse.go`) to update the UI in real-time.
    - **Cancellation**: Sends a DELETE request to `/api/chat/{sessionId}/call` to cancel ongoing streaming, which is handled by the `cancelCall` function in `call_manager.go` on the backend.
  - **`useSessionLoader.ts`**: Encapsulates the logic for loading existing chat sessions and possibly streaming the ongoing call.
    - **Backend Connection**: Establishes an SSE connection to the `/api/chat/{sessionId}` endpoint via the `loadSession` function to receive session history and real-time updates.
    **Authentication**: Fetches user information from the `/api/userinfo` endpoint via `fetchUserInfo` to verify login status.
  - **`useWorkspaceAndSessions.ts`**: Encapsulates the logic for fetching workspace and session data.
    - **Backend Connection**: Fetches workspace and session lists from the `/api/chat` (sessions list) endpoint via the `fetchSessions` function.
  - **`useCommandProcessor.ts`**: Handles processing of user commands (e.g., `/compress`).
  - **`useDocumentTitle.ts`**: Manages dynamic updates to the browser document title.
  - **`useEscToCancel.ts`**: Implements logic for cancelling operations using the Escape key.
  - **`WorkspaceContext.tsx`**: Provides a React Context for workspace-related data, often used in conjunction with `useWorkspaceAndSessions.ts`.

## General Project Concepts and Cautions

- **Language**: Code and comments are in English. User responses should be in the requested language (currently Korean).
- **Terminology**: The terms "agent" or "angel" refer to the LLM model.
- **Development**: Features may require modifications to both Go and TypeScript code. Aim to refactor significant duplications or similar structures.
- **Build:** Always run the minimal necessary build command: `npm run build-frontend` (frontend-only), `npm run build-backend` (backend-only), or `npm run build` (both). Never `npm start` (user responsibility). Run without prompt.
- **Port Configuration:** The application port can be changed to a limited extent via configuration.
- **Tests:** `npm run test` (backend-only). The backend tests cover various functionalities, including:
  - **Test Utilities (`test_utils.go`)**: Provides helper functions for setting up the test environment (`setupTest`), parsing SSE streams (`parseSseStream`), and sending HTTP requests (`testRequest`, `testStreamingRequest`).
  - **Core Backend Logic**: Tests the correct management of conversation threads, message linking, branching logic, streaming response consolidation, client synchronization with ongoing streams, API call cancellation, workspace and session management, token counting, prompt evaluation, and MCP configuration management.
  - **LLM Mocking**: `MockLLMProvider` is used to simulate LLM responses for controlled testing. The `angel-eval` model also serves as a specialized LLM for testing and debugging purposes, allowing for controlled streaming of responses.
- **Dependency**: Minimize new dependencies. Clearly explain why any new dependency is required.
- **Comments**: Comments are strictly for future maintainers, explaining *why* complex or non-obvious code exists, not *what* it does. Avoid comments that describe new features, temporary changes, or are only relevant during code generation.
- **Modern Practices:** Prioritize current, idiomatic patterns and best practices for relevant frameworks/languages, ensuring up-to-date, performant, and maintainable solutions, unless contradicted by project conventions.
- **File Operations (`replace`, `write_file`)**:
  - `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines.
  - For complex modifications, prefer `write_file` (read file, modify in memory, then overwrite).
  - **Caution for Agents**: When providing string literals containing newlines to `replace` or `write_file` tools, ensure that newlines are *double-escaped* (e.g., `\n` becomes `\\n`, `\r\n` becomes `\\r\\n`). This is to prevent issues during JSON serialization of the tool arguments.
- **Message Chain Management**: The `MessageChain` struct in `message.go` provides a high-level abstraction for managing sequences of messages in a conversation branch. It handles parent-child relationships, message ordering, generation tracking, and model management automatically. This is the preferred way to manage message addition and conversation flow.
- **Responsiveness:** Always prioritize and act on the user's *latest* input. If a new instruction arrives during an ongoing task, you *must* immediately halt the current task and address the new instruction; do not assume continuation.
- **ID Generation**: All workspace and session IDs generated by `generateID()` should contain at least one capital letter (CamelCase recommended for testing) to avoid collision with certain predefined routes.
- **Security**: While assumed to be used as a localhost application, CSRF protection is in place.

## Core Data Structures and Interaction Flow (Concise)

### Conversation Branching (`parent_message_id`, `chosen_next_id`, `branch_id`, `primary_branch_id`)
Branching allows users to explore alternative conversation paths without losing the original context. It's implemented via the `messages`, `sessions`, and `branches` tables.

- **`messages` table fields**:
  - **`parent_message_id`**: Links a message to its direct predecessor, establishing a hierarchical conversation thread.
  - **`chosen_next_id`**: Points to the `id` of the **currently selected** next message in a branch. While a parent message can have multiple divergent child messages, `chosen_next_id` explicitly designates the path that forms the active conversation flow visible to the user.
- **`branches` table**:
  - **`branch_id`**: A unique identifier for each conversation branch, used to group related messages that belong to a specific divergent path.
  - **`branch_from_message_id`**: Records the `id` of the original message from which a new branch diverged, allowing the system to understand the branching point.
- **`sessions` table**:
  - **`primary_branch_id`**: This field indicates the currently active and primary conversation flow within a given session.

**Flow**: Creating a new branch from an existing message involves generating a new message (with a `new_branch_id`) whose `parent_message_id` points back to the original message. Concurrently, the original message's `chosen_next_id` is updated to point to this new message, directing the main flow. When a user switches to a different branch, the session's `primary_branch_id` is updated, making that branch the new active one. Importantly, the `chosen_next_id` of messages in the previously active branch remains unchanged, preserving its integrity.

### Tool Execution Confirmation (`PendingConfirmation`)
This mechanism is triggered when the LLM proposes a tool call that explicitly requires user approval before execution.

- **`pending_confirmation` (in `branches` table & `InitialState`)**: This flag is set on the current branch when a tool call proposal is made by the LLM. Its state is then communicated to the frontend via the `InitialState` object.
- **Frontend**: Detects this flag and displays a clear confirmation dialog to the user, outlining the proposed tool action.
- **Confirmation Process**: User approval or denial sends a request to the backend.
  - **Backend**: The `confirmBranchHandler` endpoint (`/api/chat/{sessionId}/branch/{branchId}/confirm` POST) processes this request. It clears the `pending_confirmation` flag. If the user approves, the LLM-proposed tool (details captured within a `FunctionCall` message) is re-executed. The tool's execution result is then stored as a `FunctionResponse` message in the database, and the `chosen_next_id` of the original `FunctionCall` message is updated to point to this new `FunctionResponse`. The LLM's subsequent response streaming then resumes. If denied, a `FunctionResponse` indicating the user's rejection is added to the conversation, and the pending state terminates.
  - **Frontend**: Upon receiving the tool execution result via SSE (e.g., `EventFunctionResponse`), the UI updates accordingly, and any subsequent LLM response streams are processed.

### Environment Change Notifications (`env_changed` message type)
This mechanism informs the LLM and the user about changes to the file system "roots" accessible to the LLM, typically triggered by `/expose` or `/unexpose` commands. It ensures the LLM has an accurate operational context.

- **`session_envs` table**: Stores a historical record of "root" directories accessible to the LLM and a `generation` number for each environment. A new entry is added on each `/expose`/`/unexpose` command execution.
- **`EnvChanged` struct (in `InitialState`)**: This structure, part of the `InitialState` object sent to the frontend, details specific changes in the environment.
  - **`RootsChanged`**: Contains the full list of current roots (`value`), lists of `added` and `removed` roots, and `prompts` extracted from files like `GEMINI.md` found within new roots.
- **Operation & Display**:
  1. Executing `/expose` or `/unexpose` updates the `session_envs` table, recording a new environment `generation`.
  2. Initially, no `TypeEnvChanged` message is immediately stored in the database. Instead, the frontend receives the `EnvChanged` information via the `InitialState` object and immediately displays these changes in the UI as a **virtual message**. This virtual message is a dynamic UI element reflecting the current environment, not a persistent database entry.
  3. Only when the user **sends a new message** after an environment change, and if a difference in environment `generation` is detected, a `TypeEnvChanged` system message detailing these changes is **inserted into the database before the user's message**. This message then becomes a permanent part of the chat history.
  4. Once this `TypeEnvChanged` message is saved in the database, an `EventGenerationChanged` SSE event is sent to the frontend, signaling that a permanent record of the environment change has been created.

### Message Transmission and Streaming Endpoint Flow
Angel utilizes Server-Sent Events (SSE) for real-time, interactive conversations with the LLM.

- **SSE Backend (`sse.go`)**: The `sseWriter` manages SSE connections, sends specific events (`sendServerEvent`), and broadcasts events to all active connections for a given session (`broadcastToSession`).
- **Frontend Hooks**: `useSessionLoader.ts` and `useMessageSending.ts` are crucial custom hooks that manage SSE connections on the client side and process incoming events to update the UI in real time.

**Key Scenario Flows**:

1. **User views a session with an ongoing request (`/api/chat/{sessionId}` GET)**:
  - **Frontend (`useSessionLoader.ts`)**: When the user navigates to an existing session's URL or selects an active session from the sidebar, `useSessionLoader.ts` initiates an SSE connection to `/api/chat/{sessionId}`.
  - **Backend**: Retrieves session data and checks for any active LLM calls. If an active call is found, it sends an `EventInitialState` (including session history, current status like elapsed time, and any virtual `env_changed` data). The SSE connection remains open to continue broadcasting the LLM response stream as it progresses. If no active call, it sends `EventInitialStateNoCall` and then closes the connection.

2. **New session is created and first message is added (`/api/chat` POST)**:
  - **Frontend (`useMessageSending.ts`)**: When a user starts a new chat and sends their initial message, `useMessageSending.ts` sends a POST request to `/api/chat`.
  - **Backend**: Creates a new session, saves the user's message to the database, establishes an SSE connection, sends `EventInitialState` to inform the frontend of the initial session state, and immediately begins LLM response streaming.
  - **Frontend**: Upon receiving `EventInitialState` and the subsequent LLM response stream via its SSE connection, the UI displays the conversation content.

3. **Message is added to an existing session (`/api/chat/{sessionId}` POST)**:
  - **Frontend (`useMessageSending.ts`)**: When a user inputs and sends a message in an existing chat, `useMessageSending.ts` sends a POST request to `/api/chat/{sessionId}`.
  - **Backend**: Adds the user's message to the database. If previous `/expose` commands resulted in environment changes, a `TypeEnvChanged` message detailing these changes is inserted into the database *before* the user's message. The backend then sends an `EventInitialState` (potentially including `EventGenerationChanged` if a `TypeEnvChanged` message was added) to the frontend via SSE, updating the session's overall state. The LLM response streaming then continues from the last point.
  - **Frontend**: Upon receiving the `EventInitialState` (and `EventGenerationChanged` if applicable) and the ongoing LLM response stream via SSE, the UI updates the conversation.

4. **User approves tool usage (`/api/chat/{sessionId}/branch/{branchId}/confirm` POST)**:
  - **Frontend (`useMessageSending.ts` / `useChatSession.ts` logic)**: When the user approves a tool in the confirmation dialog, relevant frontend logic (often within `useMessageSending.ts` or `useChatSession.ts`) sends a POST request to `/api/chat/{sessionId}/branch/{branchId}/confirm`.
  - **Backend**: Executes the tool, saves its output as a `FunctionResponse` message to the conversation. An `EventFunctionResponse` event is sent to the frontend via SSE, followed by the resumption of LLM response streaming.
  - **Frontend**: Upon receiving `EventFunctionResponse` and the subsequent LLM response stream via SSE, the UI updates to reflect the tool's execution and the LLM's continued response.

### Other Key Data Structures
- **`Session`**: Contains session-wide metadata (ID, `last_updated_at`, `system_prompt`, `name`, `workspace_id`, `primary_branch_id`).
- **`Workspace`**: Defines workspace details (ID, `name`, `default_system_prompt`). `WorkspaceWithSessions` combines `Workspace` with its list of `Session`s.
- **`FileAttachment`**: Describes files attached to user messages (file name, MIME type, `hash`, Base64-encoded `data`). Backend uses refcounted blob storage via the `blobs` table, referencing files by `hash`. The `/api/blob/{blobHash}` endpoint serves files for download and display.
- **`Message` (`FrontendMessage` / `ChatMessage`)**: The fundamental unit of conversation. Includes ID, `role` (user, model), `parts` (content), `type` (`text`, `function_call`, `function_response`, `thought`, `env_changed`), `attachments`, `cumulTokenCount`, `branch_id`, `parent_message_id`, `chosen_next_id`, `possibleNextIds`, `model`, `sessionId`. `FrontendMessage` is the Go backend's representation; `ChatMessage` is the TypeScript frontend's interface.
- **`FunctionCall` and `FunctionResponse`**: Types of `Part` used when the LLM invokes a tool or receives its response. They encapsulate the tool's `name` and `args`/`response`.

# Specific instructions

- Never, ever remove the intentionally hard-coded GoogleOauthConfig!!!!
- You do NOT need to export anything in the same package!!!!
- `gemini_types.go` is for the official Gemini API types, and nothing else.
- The streaming protocol intentionally avoids `event:`.
- Feel free to use `git checkout` to roll your modification back. But do not use any other git command unless requested.
- When using `replace` or `write_file`, pay close attention to newlines and whitespace. These tools demand exact literal matches.
  - **`replace`:** `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines (`\n` or `\r\n`). Read sufficient context (via `read_file` with `limit`, or `type`/`cat`) to form accurate `old_string` and respect file's newline convention.
  - **Complex Changes:** For complex modifications prone to `replace` errors, prefer `write_file` (read file, modify in memory, then overwrite).
- **Go Error Handling:** When an error occurs, prefer re-declaring the variable using `:=` instead of `var` followed by `=` to avoid "declared and not used" errors and ensure proper variable scoping.
- **Addressing User Doubts:** If a user expresses doubt or questions a proposed solution, immediately pause the current task. Prioritize understanding the user's perspective and the reasoning behind their concerns. Engage in a dialogue to clarify their thoughts, address their points, and collaboratively arrive at a solution that aligns with their understanding and expectations. The goal is to ensure the user feels heard and confident in the approach.

