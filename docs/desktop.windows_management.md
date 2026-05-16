# Desktop Window Management

This document describes the desktop child-window runtime used by the Console SPA in Wails desktop mode.

The browser Console and the desktop app share the same Vue frontend. Desktop mode only changes the host behavior: selected dialog content can be opened in a separate WebView route instead of a `QDialog`.

## Architecture

```text
                         Wails desktop host
                 +--------------------------------+
                 | window create / reuse / hide   |
                 | parent map                     |
                 | host relay via ExecJS          |
                 +-------+----------------+-------+
                         ^                ^
                         | raw messages   | injected events
                         |                |
+------------------------+---+        +---+------------------------+
| Parent Console WebView     |        | Child WebView              |
|                            |        | /window/:window_id         |
| dialog v-model state       |        |                            |
| useDesktopPayloadDialog    |        | DesktopWindowView          |
| save initial payload       |        | take initial payload       |
| send dialog:update         |        | render dialog content      |
| handle child actions       |        | send dialog:ready/actions  |
+-------------+--------------+        +--------------+-------------+
              ^                                      ^
              | BroadcastChannel + localStorage      |
              +--------------------------------------+
                    direct frontend message bus
```

The host relay and the direct frontend bus carry the same envelope. They are both required because supported platforms do not expose the same WebView messaging features. Business messages use `_delivery_id` for deduplication. Host lifecycle messages can be sent without `_delivery_id`, so lifecycle handlers must be idempotent.

## Roles

The Wails host owns OS-level window behavior:

1. open, show, hide, focus, size, and reuse WebView windows;
2. restrict child windows to `/window` routes;
3. remember the parent window for each child window;
4. relay messages between windows through the host channel;
5. notify the parent when a child window is hidden.

The parent Console window owns business state:

1. keep the real `v-model` state for each dialog;
2. save the initial payload before opening a child window;
3. send live state updates to the child window;
4. handle child-window actions such as retry, logout, save, or selected item;
5. fall back to the normal web dialog when desktop window creation is unavailable.

The child route `/window/:window_id` owns rendering:

1. load the one-shot payload from local storage;
2. render the matching dialog content component;
3. send `dialog:ready` after it has a request id;
4. apply later `dialog:update` messages;
5. send actions back to the parent.

## Window Routes

Child windows are addressed by a stable `window_id`.

Current IDs:

1. `raw-json`
2. `poke`
3. `setup-picker`
4. `setup-connection-test`
5. `codex-auth`
6. `raw-text-editor`

The host only accepts same-origin paths under `/window`. Query parameters are used for UI hints and payload lookup only. They should not carry large business payloads.

Desktop child windows are fixed-size after opening. The caller chooses the initial width and height; users should not resize child windows by dragging the frame.

## Opening Flow

Payload dialogs use this sequence:

1. The parent wrapper calls `useDesktopPayloadDialog`.
2. The parent creates a new `request_id`.
3. The initial payload is stored in local storage with a short-lived `payload_id`.
4. The parent opens `/window/<window_id>?payload_id=...&request_id=...`.
5. The Wails host creates or reuses a named child WebView.
6. The child route takes and removes the payload from local storage.
7. The child applies the payload and sends `dialog:ready`.
8. The parent sends `dialog:update` immediately and again after short delays.
9. Further parent state changes send more `dialog:update` messages.

```text
Parent wrapper        localStorage        Wails host          Child route
     |                    |                   |                    |
     | create request_id  |                   |                    |
     | save payload       |                   |                    |
     |------------------->|                   |                    |
     | open /window/id?payload_id=...         |                    |
     |--------------------------------------->|                    |
     |                    |                   | create/reuse child |
     |                    |                   |------------------->|
     |                    |                   |                    | route load
     |                    | take payload      |                    |
     |                    |<---------------------------------------|
     |                    |                   |                    | apply payload
     |                    |                   |                    | send dialog:ready
     |<-----------------------------------------------------------|
     | send dialog:update                                        |
     |----------------------------------------------------------->|
     |                    |                   |                    | apply update
```

Raw JSON also uses a one-shot payload. It does not keep a parent `v-model` state, so it does not need the payload-dialog lifecycle.

Poke opens as a child route without an initial payload. It posts to the runtime API from the child window.

If the payload is missing, expired, or has the wrong kind, the child route must show the empty-window state and log the payload failure. Large payloads should not be moved into the URL as a fallback.

## Message Envelope

Window messages use a small JSON envelope:

```json
{
  "target": "parent",
  "window_id": "codex-auth",
  "type": "dialog:update",
  "request_id": "request-id",
  "_delivery_id": "delivery-id",
  "payload": {}
}
```

Fields:

1. `type`: required message type.
2. `target`: optional target hint. `parent` routes to the recorded parent window. `self` routes to the source window.
3. `window_id`: child window id. When `target` is empty, the host uses this to find the child window.
4. `request_id`: dialog instance id. Payload dialogs must ignore mismatched request ids.
5. `_delivery_id`: generated delivery id used for business-message deduplication.
6. `payload`: message-specific data.

Common message types:

1. `dialog:ready`: child to parent, sent after payload load.
2. `dialog:update`: parent to child, sends current dialog state.
3. `dialog:close`: parent to child, asks the child to hide.
4. `dialog:closed`: child to parent, used by content-level close buttons.
5. `desktop:window-hidden`: host to parent, sent when a child WebView is hidden through the window close hook.

Message naming rules:

1. `dialog:*` is reserved for dialog lifecycle and state transport.
2. `desktop:*` is reserved for host lifecycle notifications.
3. Dialog-specific actions use `<window_id>:<action>`.
4. Runtime actions not owned by a dialog may use their runtime namespace, such as `runtime:*`.

Dialog-specific action messages:

1. `setup-picker:selected`
2. `setup-connection-test:retry`
3. `codex-auth:logout`
4. `raw-text-editor:save`
5. `runtime:poke-submitted`

## Delivery Paths

Messages are sent through two paths. This is intentional, not a temporary fallback. Some WebView platforms support the direct frontend bus well; others are more reliable through the Wails host. Sending through both paths gives the same API to the application layer.

The direct frontend bus uses `BroadcastChannel` plus a `localStorage` fallback. It works between same-origin WebViews and is the fast path. Before sending, messages are converted to plain JSON so Vue proxies or other non-cloneable objects cannot break `BroadcastChannel`.

The host relay uses Wails raw messages. The sender posts `mistermorph:window-message:<json>` to the host. The host resolves the target window and injects a `mistermorph:desktop-window-message` event into that window.

Receivers subscribe through `onDesktopWindowMessage`. Each WebView installs one physical listener for the direct bus and one for the host relay, then fans out accepted messages to local subscribers. Business messages must be safe to receive twice. The direct bus generates `_delivery_id`, and the host relay preserves it, so duplicate delivery from the direct bus and the host relay is harmless.

Host lifecycle messages such as `desktop:window-hidden` may not have `_delivery_id`. Handlers for these messages must be idempotent: closing an already closed dialog, clearing an already cleared timer, or receiving the same hidden event twice must be safe.

## State Rules

The parent window is the source of truth for dialog state.

Child windows are hidden, not destroyed. Reopening the same `window_id` reuses the existing WebView and sets a new URL. Because hidden-window notifications can be timing-sensitive on some WebView platforms, parent open handlers must be reentrant: if the parent flag is still `true`, set it to `false`, wait one Vue tick, then set it to `true` again.

Payload dialogs use `request_id` to separate instances. A child must ignore updates for a different request id. A parent must ignore child actions for a different request id.

The initial payload is one-shot. Live state must flow through messages after `dialog:ready`.

Close and reopen use this sequence:

```text
User                 Child WebView         Wails host          Parent wrapper
 |                        |                    |                    |
 | closes child window    |                    |                    |
 |----------------------->|                    |                    |
 |                        | close hook         |                    |
 |                        |------------------->|                    |
 |                        |                    | hide child         |
 |                        |                    | notify hidden      |
 |                        |                    |------------------->|
 |                        |                    |                    | close v-model
 |                        |                    |                    | clear timers
 |                        |                    |                    |
 | clicks open in parent  |                    |                    |
 |--------------------------------------------------------------->|
 |                        |                    |                    | if still open:
 |                        |                    |                    |   set false
 |                        |                    |                    |   wait one tick
 |                        |                    |                    | set true
 |                        |                    |                    | new request_id
 |                        |                    |                    | open window
 |                        |                    |<-------------------|
 |                        |<-------------------| reuse child        |
```

## Adding a Payload Dialog

1. Extract the dialog body into a content component.
2. Keep the web wrapper as the fallback `QDialog`.
3. Add a stable `window_id` constant.
4. Add an open helper that stores the initial payload and opens `/window/<window_id>`.
5. Add one entry to the `DesktopWindowView` dialog registry.
6. Use the registry entry to define the content component, stored payload behavior, live state behavior, props, and action handlers.
7. Use `useDesktopPayloadDialog` in the wrapper.
8. Scope all action messages with `request_id`.
9. Make the parent open action reentrant through the shared reentrant-dialog helper.
10. Test browser fallback, desktop open, close, reopen, and action messages.

A dialog may need both live payload state and action messages. Treat them as independent capabilities:

1. Initial payload: one-shot data needed to render the first frame.
2. Live state: parent-to-child `dialog:update` messages for loading, errors, and results.
3. Runtime action: child-to-parent `<window_id>:<action>` messages such as retry, logout, save, or select.
4. Close event: `dialog:closed` for content-level close buttons, plus `desktop:window-hidden` for OS window close.

## Debugging

Desktop logs use the `desktop_window` prefix.

Useful events:

1. `open_payload_window`
2. `open_window_request`
3. `open_window_create`
4. `open_window_reuse`
5. `payload_taken`
6. `desktop_window_apply_payload`
7. `relay_message`
8. `notify_hidden`
9. `payload_dialog_hidden`

If a payload dialog cannot reopen, check whether `notify_hidden` is followed by `payload_dialog_hidden`. If not, the host saw the close event but the parent frontend did not reset its dialog state.
