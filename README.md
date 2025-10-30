# 😇 Angel

An experimental web-based personal LLM agent based on [gemini-cli]. Your mileage may vary.

[gemini-cli]: https://github.com/google-gemini/gemini-cli/

## Features

* Model selection between Gemini 2.5 Flash and Pro among others
  - Leverages the free tier available through Gemini Code Assist via Google
  - OpenAI-compatible API support (Ollama, custom endpoints)
* Multiple sessions and workspaces
  - Configurable system prompt per session
  - Automatic session name inference
  - Session renaming and workspace moving
* Conversation branching and editing
  - Message editing and retry functionality
  - Error recovery with retry buttons
* Thought display
* File upload, either by drag-and-drop or file picker
  - Automatic image resizing (togglable)
* Context compression (`/compress`) and history clearing commands (`/clear`, `/clearblobs`)
* Tool execution with MCP support and confirmation
  - Rudimentary filesystem tools with configurable roots (`/expose`, `/unexpose`) and syntax highlighting & diffing
  - Rudimentary shell command tools, polling supported for long-running commands
  - Web fetch tool with text extraction and summarization
  - Chat history search (`search_chat`) and binary content recall (`recall`)
  - Subagent and nanobanana-powered image generation support
  - Dedicated to-do tool
* Responsive UI with mobile support
* Full-text search across conversations
* 100% vibe-coded because why not
