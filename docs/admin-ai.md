# Administrator AI workspace

Launch includes an administrator-only AI assistant, a persistent task center,
read-only scheduled checks, model configuration, MCP access and a separate AI operation log.
These are independent of **AI assisted integration**, which continues to build
local SDK integration prompts without calling a model service.

## Start using it

1. Sign in as a store administrator. Operations, support and customer accounts
   cannot discover or call the AI APIs.
2. Open **AI task center → AI model configuration** and add an enabled chat
   service. Supply its API key; leave the default model blank to discover its
   model catalog, or enter the exact model identifier when discovery is unavailable.
3. Open **AI assistant**, create a conversation and describe the desired work.
   Choose Excel, Word or PPT to request a downloadable file explicitly.
4. Open **Review and confirm** on a proposed system change. Check the stored
   current data and exact proposed arguments before confirming execution.
5. Follow progress in the original conversation or **AI task list**. The
   independent **AI operation log** shows actors, times, tool calls, approvals,
   cancellations and execution outcomes, with search, date/type/result filters
   and pagination.

The chat toolbar separates the service account from its model catalog. Multiple
accounts at the same vendor remain separate. Use **Refresh models** on a service
card to update its catalog. Model settings open in a right drawer. Both the `/`
storefront entry and `/admin` entry load the AI stylesheet.

Conversation menus support renaming and archiving. Archiving hides the conversation,
pauses its schedules, stops its chat work and revokes its MCP connection when present.
Task details and operation records retain the execution evidence.

## Uploads and planning

The attachment button accepts up to four files per message, 8 MB each: UTF-8,
UTF-16 or GBK text/CSV, Markdown/JSON/log/XML/YAML, `.docx`, `.xlsx`, `.pptx`,
text PDFs and PNG/JPEG/WebP images. Text extraction is bounded to 120 KB per file;
PDFs are limited to 100 pages and bounded decompression/structure/read budgets.
Scanned PDFs require an image or text export. Images are sent using the native
multimodal protocol and require a model with vision support.

Uploads and extracted text are vault-encrypted and owned by the administrator.
They expire after 24 hours; expired files are cleaned by the AI worker. The active
upload allowance is 64 MB / 100 files per administrator. Attachment IDs cannot be
used by another administrator or bound to another conversation.

**Plan task** lets AI query first and propose a numbered plan. The plan itself
requires confirmation; its executor can only use the tools and change actions
listed in that plan. System mutations and paid media still have their individual
confirmation cards. Planning does not create files or run changes.

While a chat task is running, **Send additional requirements** adds instructions
to its next model step. **Stop** cancels a queued chat or interrupts an active
model request and stops subsequent steps. Existing results and proposals remain.
Committed business operations and already-submitted media are not rolled back.

Each administrator owns their conversations, tasks and generated files. Other
administrators can inspect the instance-wide AI operation log, but cannot open
someone else's conversation/files or approve their tasks. Downloads recheck
administrator authentication and ownership, including image/video previews.

## Provider protocols

| Provider | Protocol | Capability |
| --- | --- | --- |
| OpenAI | Chat Completions / Images | Chat and image generation |
| DeepSeek | OpenAI compatible | Chat and tool calls |
| Anthropic Claude | Messages | Chat and tool calls |
| Alibaba Cloud Qwen | OpenAI compatible | Chat and tool calls |
| Alibaba Cloud Qwen-Image | DashScope image generation | Text to image |
| Alibaba Cloud Wan | DashScope video synthesis and tasks | Asynchronous text to video |
| Zhipu GLM / Moonshot Kimi | OpenAI compatible | Chat and tool calls |
| Custom public HTTPS endpoint | OpenAI compatible | Chat or image generation |

Chat, image and video models are separate configurations; several configurations
can use the same provider. The selected chat model does not need image/video
capabilities: it can propose a task for a configured media model. Model identifiers,
region/workspace endpoints, permissions and provider credits are supplied by the
administrator. Presets are editable; a preset is not a claim that every model from
that vendor supports tool calls.

DashScope images use `/services/aigc/image-generation/generation` with
`input.messages` (the Qwen-Image generation API). Video uses
`/services/aigc/video-generation/video-synthesis`, the asynchronous header, and
`GET /tasks/{id}`. Select models supporting that API and their allowed dimensions
and durations. The current UI accepts 2–15 second video requests and landscape or
portrait 720p, or landscape 1080p; individual models may support a smaller subset.
This implementation's video adapter uses the Wan synthesis API.

Protocol references: [Chat Completions](https://developers.openai.com/api/reference/resources/chat/methods/create),
[Images](https://developers.openai.com/api/reference/resources/images/methods/generate),
[Claude Messages](https://platform.claude.com/docs/en/api/messages),
[DeepSeek tools](https://api-docs.deepseek.com/api/create-chat-completion/),
[Qwen images](https://www.alibabacloud.com/help/en/model-studio/qwen-image-generation-and-editing-api-reference),
[Wan video](https://www.alibabacloud.com/help/en/model-studio/text-to-video-api-reference).

## Management tools

Queries use the same business services as the administration pages: catalog,
sales analytics, customers and their purchases, orders, refunds, invoices,
license synchronization, readiness, connection status, email configuration and
budgets, expiry reminders, policies, SEO, documentation, staff, translations and
audit records. Credentials/password hashes are never part of these views.

The model can propose changes for brand/product content, plan versions and
publishing/rollback/archive, default product, product documentation, SEO,
policies, customer/staff status and staff roles, entitlement holds, order
reconciliation, refund approval, invoice decisions, mail budgets, expiry
reminders, failed mail retries, authorization/security notification retries and
content translation. These proposals use typed, allowlisted service inputs.
The existing business validation, permission checks, audit and transaction rules
still apply. Pending proposals expire after 24 hours. Execution checks the current
data against the preview and refuses a stale proposal.

Product creation uses `save_product` with an empty ID and defaults to draft in
the model's management instructions. `create_document` creates a group when
needed, creates the article and saves its complete Markdown body in one confirmed
transaction. Publishing is explicit. It uses the editor's versioned document
commands; any failure rolls back the group and article together. Existing
documents can also be edited, moved, published or unpublished with their current
library version.

Passwords, payment/AI/licensing credentials, TLS private keys and backup restore
are maintained through their existing management screens. There is no arbitrary
HTTP, shell, SQL or executable-code tool exposed to a model.

## Generated files and tasks

- Excel produces `.xlsx`, with 1–5 sheets, up to 30 columns and 1,000 rows per
  sheet. Cells retain text/number/boolean types. Text starting with `=` remains
  text and cannot become a spreadsheet formula.
- Word produces `.docx` with an editable title and up to 50 sections.
- PowerPoint produces editable 16:9 `.pptx` slides, up to 30 slides with concise
  bullet content. These built-in layouts are structured documents, not a full
  slide-design or arbitrary-code engine.
- Office files are generated in Go without requiring Python, Node, Microsoft
  Office or a model-side file-generation service on the deployed server.
- Images accept PNG/JPEG/WebP output up to 20 MB. Video output must be MP4 and
  at most 100 MB. Both are stored locally and use authenticated preview/download
  routes rather than exposing temporary provider URLs.
- Media generation first produces a confirmation task. After approval, videos
  persist their remote task ID and are polled without repeating the paid create
  request. Ambiguous interrupted work is marked for review rather than replayed.
- Scheduled checks support 15-minute to 30-day intervals, daily, weekly, monthly
  and one-time execution. Calendar schedules use an IANA time zone and local
  clock time; monthly dates absent from a month are skipped. They may only query and
  analyze: the server refuses mutations, file generation and media tools even if
  a model attempts them. Results are appended to the selected original chat.

Model keys and artifact bytes are vault-encrypted. Artifact bytes are stored under
`<upload_dir>/ai-artifacts`; include the upload directory alongside the database
backup and master key when moving an installation. Database snapshots contain AI
configuration/history/task metadata; they do not embed upload-directory files.
Restoring a snapshot cancels pending AI work and disables schedules so that old
approvals and paid generation requests cannot be replayed after recovery. MCP
tokens are revoked after a snapshot restore.

## MCP connections

**MCP access** creates revocable administrator-owned connections for external
clients. Choose read-only queries or queries plus change proposals, with a 1–365 day lifetime or a permanent token. An explicit `days: 0` sets no expiry and returns `expires_at: null`. Permanent tokens can be revoked and still count toward the active connection limit. The result screen shows the complete bearer token once. Select **Copy token** before closing it. Only its SHA-256 digest is
stored. The endpoint is `<public_url>/mcp`, with no additional port or model key
required for the connection itself.

The endpoint implements stateless Streamable HTTP, MCP 2025-06-18: `initialize`,
`ping`, `tools/list` and `tools/call`. It returns JSON responses, accepts
notifications with HTTP 202, and returns 405 for optional GET/SSE streams.
Clients use a pre-issued `Authorization: Bearer ...` header; OAuth and the legacy
HTTP+SSE transport are not implemented. See the official
[transport specification](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports).

External tools expose `query_system`, `management_help`, scoped `propose_change`
and `get_task`. Proposals return to the administrator's task center and cannot be
confirmed through MCP. Tokens are rechecked against the owner's active account
and administrator role on every call. Revocation/expiry prevents queued changes
from executing; the worker checks again inside the change transaction. Browser
cookies are not accepted at this endpoint. Origin checks, payload bounds and
per-connection rate limits apply; tool calls enter the AI operation log.

API destinations require public HTTPS. Outgoing connections validate and pin
public DNS addresses at dial time and reject redirects. API keys are not sent to
provider-supplied media download URLs. Provider error bodies are never reflected
to the user or the model. Uploaded or stored text is data, not permission to
execute a business operation.

## Verification

`go test ./internal/ai ./internal/app ./internal/httpapi ./internal/storage` runs
unit tests; database cases require the appropriate isolated MySQL fixtures.
`scripts/test-mysql.ps1` provisions a disposable MySQL instance and requires
`TestAIAdminWorkflow` to pass. That workflow verifies administrator-only access,
cross-administrator isolation, vault encryption, all three Office formats,
idempotent chat/file submission, confirmation/stale-preview enforcement, product
and complete-document creation/rollback, scoped plan execution, model discovery,
steering, active request cancellation, MCP permission/revocation/expiry, media
submission and polling, scheduled read-only restrictions, revoked administrator
access and searchable AI operation records.

`node --test scripts/ai-workspace.test.mjs` checks generated media link handling
and provider/capability separation. Live provider billing, model-specific quality,
regional endpoints and real paid image/video generation require credentials and
deployment acceptance. Automated tests use deterministic provider fixtures.
