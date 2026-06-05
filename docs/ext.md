# Extension Points

This document lists the extension points intended for downstream builds.

The rule is simple: upstream keeps generic hooks, downstream keeps product-specific code. The default upstream build must behave the same when no extension is provided.

## Console Backend Routes

Console backend API routes are registered in `cmd/mistermorph/consolecmd`.

Downstream code in the same Go package can register extra routes during `init()`:

```go
package consolecmd

import "net/http"

func init() {
	registerConsoleRouteRegistrar(func(mux *http.ServeMux, srv *server, apiPrefix string) {
		mux.HandleFunc(apiPrefix+"/pro/skill-store/skills", srv.withAuth(handleProSkills))
	})
}

func handleProSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}
```

Notes:

- `apiPrefix` already includes `console.base_path`. With `console.base_path: /console`, the API prefix is `/console/api`.
- Registrars run after upstream API routes are registered and before the SPA fallback is registered.
- Use `srv.withAuth(...)` for protected routes.
- This is a same-package extension point. It does not export `server` as a public API.
- Duplicate route patterns will fail through `http.ServeMux`, so downstream routes should use their own prefix such as `/pro/...`.

## Console Frontend Routes

Route extensions live at:

```text
web/console/src/ext/routes/index.js
```

The upstream default is empty:

```js
export const routeExtensions = {
  routes: [],
  setupFreePaths: [],
};
```

Downstream builds can provide the same module shape:

```js
export const routeExtensions = {
  routes: [
    { path: "/skill-store", component: () => import("pro-console/views/SkillStoreView") },
  ],
  setupFreePaths: [],
};
```

`routes` is passed to Vue Router. Extension routes are mounted after upstream routes and before the root redirect. They should add new pages, not replace upstream pages.

`setupFreePaths` is added to the setup guard allowlist. It only bypasses the setup-completion guard. It does not make a route public; normal Console auth still applies unless the route itself is public.

## Console Frontend Slots

UI slots live at:

```text
web/console/src/ext/slots/index.js
```

The current slot ids are:

| Slot id | Location | Props |
| --- | --- | --- |
| `sidebar.before_runtime` | In the sidebar nav list, immediately before the Runtime nav item. | `selectedEndpointItem`, `currentPath`, `mobile`, `t` |
| `sidebar.bottom_left` | At the lower part of the desktop sidebar and mobile drawer. | `selectedEndpointItem`, `currentPath`, `t` |

The upstream default exports `null` entries. A `null` slot renders nothing.

Example downstream module:

```js
import SkillStoreNavEntry from "pro-console/components/SkillStoreNavEntry";
import AccountBadge from "pro-console/components/AccountBadge";

const uiSlots = {
  "sidebar.before_runtime": SkillStoreNavEntry,
  "sidebar.bottom_left": AccountBadge,
};

export { uiSlots };
```

Slot components use the same JavaScript Vue component style as the rest of the Console. They may import existing frontend helpers such as API functions, stores, or `translate`.

## Console i18n

i18n extensions live at:

```text
web/console/src/ext/i18n/index.js
```

The upstream default is empty:

```js
export const i18nExtensions = {};
```

Downstream builds can provide extra message keys:

```js
export const i18nExtensions = {
  en: {
    skill_store_title: "Skill Store",
    skill_store_installed: "Installed",
  },
  zh: {
    skill_store_title: "技能市场",
    skill_store_installed: "已安装",
  },
  ja: {
    skill_store_title: "Skill Store",
    skill_store_installed: "インストール済み",
  },
};
```

Lookup order:

1. Current locale in upstream core messages.
2. English upstream core messages.
3. Current locale in `i18nExtensions`.
4. English `i18nExtensions`.
5. The key itself.

Core messages win over extension messages. Use extension-specific key names to avoid accidental collisions.

Variable replacement uses the existing `{name}` syntax:

```js
translate("skill_store_count", { count: 3 });
```

## Providing Frontend Extensions

Downstream builds can provide these modules either by source overlay or by Vite alias. The replacement module must export the same names:

- `routeExtensions` from `web/console/src/ext/routes`
- `uiSlots` from `web/console/src/ext/slots`
- `i18nExtensions` from `web/console/src/ext/i18n`

Keep extension modules small. They should register routes, slots, or messages. Product logic belongs in downstream components and API handlers.

## MarkdownEditor Read-Only State

`MarkdownEditor` supports separate `disabled` and `readOnly` states:

```vue
<MarkdownEditor v-model="body" readOnly />
<MarkdownEditor v-model="body" :disabled="saving" :readOnly="locked" />
```

Semantics:

- `disabled`: disabled form control. The editor cannot be focused or edited.
- `readOnly`: enabled form control. Text remains selectable and scrollable, but cannot be edited.
- If both are true, `disabled` wins.

This is not a registry hook, but downstream route views and slot components can rely on it for read-only markdown surfaces.
