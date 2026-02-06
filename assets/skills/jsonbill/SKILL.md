---
name: jsonbill
description: Generate a PDF invoice/statement from JSON via JSONBill API.
auth_profiles: ["jsonbill"]
requirements:
  - http_client
  - file_io
  - optional: file_send (chat)
---

# JSONBill (portable, safe)

API base: `https://api.jsonbill.com`

This skill is designed to call the JSONBill REST API **without ever exposing API keys/tokens to the LLM**:

- Do **not** ask the user to paste an API key/token.
- Do **not** place secrets in prompts, logs, or tool parameters.
- Use a **host-side credential mechanism** (e.g. “credential profile”, “secret manager”, “vault”, env var injection) so the agent never sees the secret value.

## Prerequisites (host configuration)

The host must be configured (outside this skill) with:

- A credential profile id (recommended): `jsonbill`
- A secret reference (example): `JSONBILL_API_KEY`
- A binding that injects the credential into HTTP requests (commonly: `Authorization: Bearer <secret>`)

## Endpoints

- Create task: `POST /tasks/docs`
- Poll task: `GET /tasks/{trace_id}`
- PDF URL: `GET /tasks/{trace_id}.pdf` (usually you can return this URL without downloading)

## Portable pseudocode

Notes:
- If the user input is a **local file path**, you MUST read the file and parse its JSON first. Never send local paths to external APIs.
- Prefer downloading the PDF and sending the file if the platform supports it; otherwise return the PDF URL.

```text
Inputs:
  invoice_source: either (a) JSON object, or (b) local file path to a JSON file

Definitions (abstract operations; map to your agent/tooling):
  READ_TEXT(path) -> string
  PARSE_JSON(text) -> object
  HTTP_JSON(method, url, auth_profile, json_body, headers?) -> {status, json, text}
  HTTP_BYTES(method, url, auth_profile, headers?) -> {status, bytes, content_type}
  SLEEP(seconds)
  SAVE_BYTES(path, bytes) -> saved_path
  SEND_FILE(saved_path, filename, caption?)  (optional; if your platform supports files)

Procedure:
  if invoice_source is a local path:
    invoice_text = READ_TEXT(invoice_source)
    invoice_json = PARSE_JSON(invoice_text)
  else:
    invoice_json = invoice_source

  # 1) Create async generation task
  create = HTTP_JSON(
    method="POST",
    url="https://api.jsonbill.com/tasks/docs",
    auth_profile="jsonbill",
    json_body=invoice_json
  )
  assert create.status is 2xx
  trace_id = create.json.trace_id (or create.json.data.trace_id depending on API)
  assert trace_id is not empty

  # 2) Poll until done (backoff recommended)
  interval = 1.0
  deadline_seconds = 60
  elapsed = 0
  while true:
    poll = HTTP_JSON(
      method="GET",
      url="https://api.jsonbill.com/tasks/" + trace_id,
      auth_profile="jsonbill",
      json_body=null
    )
    assert poll.status is 2xx
    status = poll.json.status
    if status == 3:
      break
    if status == 4:
      raise error with poll.json (or poll.text)
    SLEEP(interval)
    elapsed += interval
    if elapsed >= deadline_seconds:
      raise timeout
    interval = min(interval * 1.5, 8.0)

  # 3) PDF download
  pdf_url = "https://api.jsonbill.com/tasks/" + trace_id + ".pdf"
  if platform supports sending files:
    pdf = HTTP_BYTES(
      method="GET",
      url=pdf_url,
      auth_profile="jsonbill",
      headers={"Accept": "application/pdf"}
    )
    assert pdf.status is 2xx
    assert pdf.bytes is non-empty
    saved = SAVE_BYTES("jsonbill/invoice_" + trace_id + ".pdf", pdf.bytes)
    SEND_FILE(saved, filename="invoice.pdf", caption="Your PDF invoice")
  else:
    return pdf_url
```

## Status semantics (expected)

| Status | Meaning | Action |
|---:|---|---|
| `3` | Completed | Return PDF URL. |
| `4` | Error/failed | Abort and surface error details. |
| other | Pending/in-progress | Keep polling until timeout. |
