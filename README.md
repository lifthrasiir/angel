# 😇 Angel

An experimental web-based personal LLM agent based on [gemini-cli]. Your mileage may vary.

[gemini-cli]: https://github.com/google-gemini/gemini-cli/

## Features

* Model selection between Gemini 2.5 Flash and Pro
  - Leverages the free tier available through Gemini Code Assist via Google
* Multiple sessions and workspaces
  - Configurable system prompt per session
  - Automatic session name inference
* Thought display
* File upload
* Context compression (`/compress`)
* Tool execution with MCP support and confirmation
  - Rudimentary filesystem tools with configurable roots (`/expose`, `/unexpose`)
  - Rudimentary shell command tools, polling supported for long-running commands
  - Dedicated to-do tool
* 100% vibe-coded because why not
