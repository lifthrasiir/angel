# Key Concepts and Cautions for Agent Working on this Codebase

- **Project:** Angel - A personalized coding agent using Go and React/TypeScript.
- **Goal:** Create a simple, single-user web version of `@google/gemini-cli`.
- **Default LLM Model:** `gemini-2.5-flash` (with support for other Gemini 2.5 and 3 models)

## Project Features

- **Flexible LLM Integration**: The application supports multiple ways to connect to LLMs. It can use the Gemini Code Assist API (via Google OAuth) for free-tier access, or connect directly to the Gemini API using a standard API key.
- **Multi-session and Workspaces**: Users can create and manage multiple chat sessions, which can be organized into distinct workspaces.
- **Branching**: Users can create new conversation branches from existing user messages and switch between different branches within a session, allowing for exploration of alternative conversation paths. This is managed by linking messages using `parent_message_id` and `chosen_next_id` in the `messages` table, and associating them with specific `branch_id`s. The `primary_branch_id` in the `sessions` table indicates the currently active conversation path.
- **Configurable System Prompt per Session**: Each chat session allows for a custom system prompt. These prompts support Go templating for dynamic generation using the `EvaluatePrompt` function, with a preview feature available in the UI.
- **Automatic Session Name Inference**: The LLM can infer and update session names based on conversation content. This process is handled by `inferAndSetSessionName`, which uses specific prompts (`GetSessionNameInferencePrompts`) to guide the LLM.
- **Thought Display**: The LLM's internal thought processes are streamed as "thought" messages to the user interface. These thoughts are grouped and can be expanded/collapsed for detailed viewing. The `Part` struct includes a `Thought` field, and these messages are broadcast via the `EventThought` SSE event.
- **File Upload and Attachment**: Users can attach files to their messages. Files are Base64 encoded during transmission, but stored as raw binary data in a refcounted blob storage system via the `blobs` table, and support preview and download functionalities via the `/api/blob/{blobHash}` endpoint. The blob system automatically manages reference counting to prevent data loss when files are shared across multiple messages. Images support automatic resizing (togglable) to optimize file sizes.
- **Tool Usage**: The LLM can invoke both built-in tools (e.g., `list_directory`, `read_file`, `write_file`, `run_shell_command`, `write_todo`, `web_fetch`, `subagent`, `generate_image`, `search_chat`, `recall`) and external tools via the Model Context Protocol (MCP). The `web_fetch` tool can extract URLs, check for private IPs, convert GitHub blob URLs to raw URLs, and includes a fallback mechanism for direct fetching. The `write_file` tool returns a unified diff for verification. The `subagent` tool allows spawning specialized subagents with custom system prompts for specific tasks. The `generate_image` tool enables image creation and editing using subagents with image generation capabilities. The `search_chat` tool searches through chat history using keywords, returning matching messages with context excerpts. The `recall` tool retrieves unprocessed binary content for internal AI processing using SHA-512/256 hashes. Directories can be exposed or unexposed via /(un)expose commands, and their contents and per-directory directives (GEMINI.md etc.) are given as dynamic system prompts.
- **Chat History Compression**: The chat history can be compressed into a concise, structured XML snapshot by the user explicitly running the `/compress` command. This process is handled by the `CompressSession` function, which uses token thresholds to determine when to summarize and updates the message chain with the compressed content.
- **History Context Management**: Users can clear chat history or blob storage using the `/clear` and `/clearblobs` commands for context alternation and storage management.

## Go Backend

- **Authentication (`src/gemini/code_assist.go`, `src/gemini/google_login.go`)**: Manages user authentication. It supports two main methods: Google OAuth for the free-tier Code Assist API and direct API key authentication for the Gemini API.
- **Database (`src/database`)**: Uses SQLite (`angel.db`) for persistent storage of sessions, messages, and other configurations. The `messages` table utilizes `parent_message_id`, `chosen_next_id`, and `branch_id` to manage conversation threads and branching. The `blobs` table stores file data with automatic reference counting via triggers. Functions like `AddMessageToSession` and `UpdateMessageContent` are crucial for message management. The database includes full-text search capabilities for chat history and periodic WAL checkpointing for performance optimization.
- **Chat Logic (`chat_*.go`)**: Includes `chat_branch.go`, `chat_command.go`, `chat_stream.go` and so on for better modularity.
- **Gemini API Interaction (`src/gemini/gemini_api.go`, `llm.go`, `models.go`)**:
  - Defines the `LLMProvider` interface in `llm.go` for abstracting interactions with various Large Language Models, including methods like `SendMessageStream` (for streaming responses), `GenerateContentOneShot` (for single-shot responses), `CountTokens` and `MaxTokens`.
  - `SessionParams` in `llm.go` holds all parameters for an LLM chat session, including `Contents`, `ModelName`, `SystemPrompt`, `IncludeThoughts`, `GenerationParams` (e.g., Temperature, TopK, TopP), and `ToolConfig`.
  - `src/gemini/types.go` strictly defines official Gemini API types such as `Content`, `Part`, `Schema`, etc.
  - The `CodeAssistClient` and a new `GeminiClient` in the `src/gemini/` directory facilitate communication with Google's APIs.
- **Model Management (`models.go`, `models.json`)**: Model definitions (names, token limits, capabilities) are loaded at startup from `models.json`. The `Models` in `models.go` manages access to these model definitions.
- **OpenAI-Compatible LLM Support (`openai.go`)**: The `OpenAIClient` implements the `LLMProvider` interface for OpenAI-compatible APIs, supporting streaming chat completions, function calling, and automatic context length probing for models like Ollama. The system can dynamically register multiple OpenAI models from database configurations.
- **Model Context Protocol (MCP) Management (`src/internal/tool/mcp.go`)**: The `MCPManager` in `mcp.go` handles connections to multiple MCP servers. It resolves naming conflicts between built-in and MCP tools by prefixing MCP tool names (e.g., `mcpName__toolName`) and dispatches tool calls via `DispatchCall`.
- **Server-Sent Events (SSE) Implementation (`sse.go`)**: The `sseWriter` in `sse.go` streams real-time updates to clients. It defines various `EventType`s (e.g., `EventInitialState`, `EventThought`, `EventModelMessage`, `EventFunctionCall`, `EventFunctionResponse`, `EventComplete`, `EventSessionName`, `EventCumulTokenCount`, `EventError`) and supports broadcasting events to active SSE clients. The streaming protocol is a custom JSON stream format, intentionally avoiding the `event:` prefix.
- **Tool Definition and Management (`src/internal/tool/tools.go`, `tools_*.go`)**: `tools.go` defines the core framework for tool management, including `Tools` registry and `Definition` (which specifies tool name, description, parameters, and handler). Built-in tools like `list_directory`, `read_file`, `web_fetch` (implemented in `tools_webfetch.go`), `subagent`, `generate_image` (implemented in `tools_subagent.go`), `search_chat` and `recall` (implemented in `tools_search_chat.go`) are managed here. `Tools.ForGemini` method prepares tools for the Gemini API, and `Tools.Call` method in `tools.go` dispatches calls to either local or MCP tools.
- **Prompt Management (`prompts.go`, `prompts_builtin.go`)**: Uses Go templating for evaluating prompts via the `EvaluatePrompt` function in `prompts.go`. `prompts_builtin.go` defines built-in prompt templates for core agent behavior (`GetDefaultSystemPrompt`), conversation summarization (`GetCompressionPrompt`), and session name inference (`GetSessionNameInferencePrompts`).
- **Project Entry Point (`main.go`)**: Initializes global state and services, including the database, MCP manager, and authentication. It also sets up the main HTTP router (`InitRouter`) and serves static files and SPA routes.

## React/TypeScript Frontend

- **Core Technologies**: Built with React, TypeScript, and Vite.
- **Key Files & Hooks**:
  - **`main.tsx`**: The application's entry point.
  - **`useChatSession.ts`**: A central hub hook that encapsulates all chat session-related state and logic.
  - **`useSessionManager.ts`**: Handles the complex logic for managing the list of workspaces and sessions in the sidebar.
  - **`useMessageSending.ts` & `useSessionLoader.ts`**: Manage sending messages and loading session history, including handling the Server-Sent Events (SSE) connection for real-time updates.
  - **`atoms/chatAtoms.ts`**: Defines Jotai atoms for global state management.
  - **`types/chat.ts`**: Contains TypeScript interface definitions for chat-related data structures.
- **UI Components**:
  - **`ChatArea.tsx`**: The central component for the chat interface.
  - **`ChatLayout.tsx`**: Defines the overall application layout.
  - **`SystemPromptEditor.tsx`**: UI for editing and previewing system prompts.
  - **`Sidebar.tsx`**: Contains the `WorkspaceList` and `SessionList`, which were significantly refactored for better state management and performance.
  - **Message Components**: A suite of components for rendering different message types (`UserTextMessage.tsx`, `ModelTextMessage.tsx`, `FunctionCallMessage.tsx`, etc.).

## General Project Concepts and Cautions

- **Language**: Code and comments are in English. User responses should be in the requested language (currently Korean).
- **Build:** Always use `npm run build`. Consider using `npm run build-frontend` and `npm run build-backend` for faster iteration during development.
- **Tests:** `npm run test` (backend-only). The backend tests cover various functionalities. Use `npm run test-backend -- -run <TestName>` for specific tests.
- **File Operations (`replace`, `write_file`)**:
  - `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines.
  - For complex modifications, prefer `write_file` (read file, modify in memory, then overwrite).
- **Message Chain Management**: The `MessageChain` struct in `message.go` provides a high-level abstraction for managing sequences of messages in a conversation branch. It handles parent-child relationships, message ordering, generation tracking, and model management automatically. This is the preferred way to manage message addition and conversation flow.
- **Responsiveness:** Always prioritize and act on the user's *latest* input. If a new instruction arrives during an ongoing task, you *must* immediately halt the current task and address the new instruction; do not assume continuation.
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
- **`src/gemini/types.go`** is for the official Gemini API types, and nothing else.
- The streaming protocol intentionally avoids `event:`.
- Feel free to use `git checkout` to roll your modification back. But do not use any other git command unless requested.
- When using `replace` or `write_file`, pay close attention to newlines and whitespace. These tools demand exact literal matches.
  - **`replace`:** `old_string`/`new_string` must exactly match, including all whitespace, indentation, and newlines (`\n` or `\r\n`). Read sufficient context (via `read_file` with `limit`, or `type`/`cat`) to form accurate `old_string` and respect file's newline convention.
  - **Complex Changes:** For complex modifications prone to `replace` errors, prefer `write_file` (read file, modify in memory, then overwrite).
- **Go Error Handling:** When an error occurs, prefer re-declaring the variable using `:=` instead of `var` followed by `=` to avoid "declared and not used" errors and ensure proper variable scoping.
- **Addressing User Doubts:** If a user expresses doubt or questions a proposed solution, immediately pause the current task. Prioritize understanding the user's perspective and the reasoning behind their concerns. Engage in a dialogue to clarify their thoughts, address their points, and collaboratively arrive at a solution that aligns with their understanding and expectations. The goal is to ensure the user feels heard and confident in the approach.