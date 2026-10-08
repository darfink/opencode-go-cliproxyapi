## Local Model Enrichment

- Use a 1,048,576-token fallback for `muse-spark-1.3-contributor` when its catalog omits the context limit.
- Preserve explicit provider limits and dynamic model discovery.
- Consolidate reasoning efforts, context fallbacks, and hosted-search policy into one model-enrichment table.
- Add `model-enrichments` configuration overrides for `reasoning-efforts`, `context-window`, and `hosted-web-search`.
- Resolve enrichment once per catalog snapshot. Explicit configuration overrides provider metadata; built-ins fill missing metadata on audited endpoints.
- Replace the local `disable-muse-hosted-search` switch with a per-model `hosted-web-search: disabled` policy.
- Apply hosted-search filtering to all Responses models, not just Muse. Preserve client-executed search functions and other models.
- Reject requests that force a disabled hosted search tool.
- Preserve original usage counters in streaming and non-streaming responses.

These changes are maintained on the fork's `el/local-opencode-go-setup` branch. They are not part of the v8 integration PR.

## Local Responses Compatibility

- Reuse the CLIProxyAPI SDK tool index for Codex Responses requests on Chat Completions and Messages routes.
- Normalize namespaced and custom tools on native Muse Spark and Grok Responses routes.
- Preserve native Responses fields, reasoning controls, media, usage, and hosted tools.
- Remove the unsupported `search_content_types` field from Muse Spark search tools.
- Preserve custom tool identities through requests, responses, streaming, and tool-result history.
- Return bounded, redacted provider errors when streaming requests fail before the first response byte.
- Close stalled error reads when the request times out.

These fixes remain on the local-setup branch, separate from the reasoning metadata and v8 integration commits.

## Local Reasoning Metadata

- Publish verified reasoning levels when OpenCode Go omits model metadata.
- Include accepted `max`, `xhigh`, and `ultra` options for the audited models and routes.
- Keep explicit model metadata authoritative. Other models retain the low, medium, and high fallback.
- Keep model discovery dynamic. The verified table supplements capabilities, not the model list.
- Forward named efforts on Chat Completions and native Responses routes.
- Reject Messages efforts that have no supported token-budget conversion.
- Keep Qwen 3.6 below `max` because that token-budget conversion fails.

These changes remain on the local branch. They are not part of the v8 integration PR.

## CLIProxyAPI v8

### Features

- Support CLIProxyAPI v8 and its generic plugin quota API.
- Return rolling, weekly, and monthly quotas for the credential selected by CLIProxyAPI.
- Advertise the quota provider as **OpenCode Go**.
- Remove the separate quota page and custom management routes.
- Accept an optional `name` for each API key.
- Use readable account labels and filenames instead of key digests.
- Preserve existing auth IDs, custom labels, filenames, and host metadata.
- Prevent duplicate auth records at cold start when readable filenames exist.
- Reject invalid quota values and redact provider request errors.

### Upgrade

1. Upgrade CLIProxyAPI to v8.0.0 or later.
2. Replace the plugin binary with the new build.
3. Restart CLIProxyAPI.

The generic endpoints are `GET /v0/management/quota/providers` and `POST /v0/management/quota/fetch`.
Both require the management key. Quota reset is unsupported.

The management dashboard must support the generic quota API to show plugin refresh controls.
Older dashboard builds do not show these controls.

Existing credential files keep their names and stable IDs.
Set `name:` on an API key to choose its display label.
New files use readable names.

## What's Changed

Full OpenAI Codex CLI support across all models: multi-agent namespaces, custom programmatic tools (`exec`), tool call results, and reasoning efforts are fully functional.

### Bug Fixes

- Fix missing Responses stream item lifecycle completion events (output_text.done, content_part.done, function_call_arguments.done, output_item.done) prior to response.completed, resolving dropped assistant output and tool calls in OpenAI Codex CLI and strict Responses clients.
- Fix handling of in-history messages with role: "system" across protocol adapters without rejecting them as unsupported roles or forwarding invalid turn roles to upstream providers that require alternating user/assistant turns.
- Fix handling of multi-part content arrays in function_call_output.output under /v1/responses decoding.
- Fix upstream HTTP >= 400 error propagation in streaming and non-streaming requests by reading and extracting the upstream error payload instead of discarding it with generic fallback errors.
- Add two-way Responses tool namespace and additional_tools translation: merge dynamic declarations from input items (type: "additional_tools"), unroll client-side grouping tools (type: "namespace") into qualified wire names for upstream models, and restore original tool names and namespaces across non-streaming output and streaming SSE events for OpenAI Codex CLI and multi-agent harnesses.
- Add Codex custom tool normalization and hosted tool dropping: rewrite generic custom tools (`type: "custom"`, e.g. `exec`) into function tools with default object schemas, and drop client-only/hosted tools (`apply_patch`, `web_search`, `web_search_preview`, `tool_search`, `image_generation`) when routing to Chat Completions or Messages endpoints, resolving Codex CLI failures on models like `space-bunny-free`.
- Add bidirectional support for `custom_tool_call` and `custom_tool_call_output` conversation items and outbound custom tool events: translate inbound custom tool calls and results across Chat Completions and Messages protocols, unwrap arguments into clean raw input, and restore `custom_tool_call` and `response.custom_tool_call_input.done` on outbound non-streaming and streaming responses, resolving Codex tool dispatch aborts on programmatic tools like `exec`.
- Remove local reasoning effort validation across request translators: allow client-declared reasoning effort levels (e.g. `xhigh`, `max`, and unlisted levels) to pass through transparently to upstream endpoints without local gatekeeping in `chatcompletions`, `responses`, and `messages` adapters.
- Normalize tools for non-GPT native Responses models: unroll namespaces, convert custom tools (`exec`) into function tools, and sanitize `web_search` definitions when targeting non-GPT Responses endpoints (e.g. `muse-spark`, `grok`), while preserving transparent passthrough for native GPT models (e.g. `gpt-6-luna`).
- Restore custom tool calls across native Responses-to-Responses streaming and non-streaming adapters, ensure historical function_call arguments default to valid JSON via `shared.DefaultArgs` to resolve Muse Spark errors, drop compaction input items to fix Grok compaction blob decode failures, and expand `UnwrapCustomToolInput` to unwrap arguments, code, cmd, and command fields.
- Drop historical `reasoning` input items for non-GPT Responses upstreams: resolve Grok HTTP 400 "Could not decode the compaction blob" decryption errors on multi-turn conversations caused by pooled account credential mismatches on encrypted reasoning blobs.


## Upgrade Notes

- Replace the old plugin binary with the new release binary.
- Restart CLIProxyAPI after replacing the plugin.
- Hard-refresh Management Center if the plugin page looks stale.

**Full Changelog**: https://github.com/massiveits/opencode-go-cliproxyapi/compare/v0.1.9...v0.1.10
