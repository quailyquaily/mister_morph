# Deploy MisterMorph with systemd

Use this target when you run `mistermorph` on your own Linux VM or bare-metal host and want a long-running service managed by systemd.

## Files

- `mister-morph.service`: hardened systemd unit template
- `mister-morph-console.service`: hardened Console API + SPA unit template

## Best for

- Full control over host/network/storage
- Stable long-running service on a single Linux machine
- Teams that already operate VM-based workloads

## Prerequisites

- Linux host with `systemd`
- A `mistermorph` binary (for example `/opt/morph/mistermorph`)
- A config file (for example `/opt/morph/config.yaml`)
- Root/sudo access to install unit files and create users

## Recommended layout

- Binary: `/opt/morph/mistermorph`
- Config: `/opt/morph/config.yaml`
- Console SPA: `/opt/morph/web/console/dist`
- Env (non-secret): `/opt/morph/morph.env`
- Env (secret): `/opt/morph/morph.secrets.env`
- State dir: `/var/lib/morph` (managed by `StateDirectory=morph`)
- Cache dir: `/var/cache/morph` (managed by `CacheDirectory=morph`)

## Install

1. Create a dedicated runtime user:

```bash
sudo useradd --system --home /var/lib/morph --shell /usr/sbin/nologin morph || true
```

2. Place binary and config:

```bash
sudo install -d -m 0755 /opt/morph
sudo install -m 0755 ./bin/mistermorph /opt/morph/mistermorph
sudo install -m 0640 ./config.yaml /opt/morph/config.yaml
```

3. Create env files (recommended):

```bash
sudo tee /opt/morph/morph.env >/dev/null <<'EOF'
MISTER_MORPH_LOG_LEVEL=info
MISTER_MORPH_SERVER_AUTH_TOKEN=replace-with-random-token
EOF

sudo tee /opt/morph/morph.secrets.env >/dev/null <<'EOF'
MISTER_MORPH_LLM_API_KEY=replace-with-real-api-key
MISTER_MORPH_CONSOLE_PASSWORD=replace-with-console-password
EOF

sudo chmod 0640 /opt/morph/morph.env
sudo chmod 0600 /opt/morph/morph.secrets.env
sudo chown root:morph /opt/morph/morph.env /opt/morph/morph.secrets.env
```

4. Install and start daemon service:

```bash
sudo install -m 0644 ./deploy/systemd/mister-morph.service /etc/systemd/system/mister-morph.service
sudo systemctl daemon-reload
sudo systemctl enable --now mister-morph
```

5. Verify:

```bash
sudo systemctl status --no-pager mister-morph
sudo journalctl -u mister-morph -f
```

## Optional: install Console service

1. Build and place SPA assets:

```bash
cd web/console
pnpm install
pnpm build

sudo install -d -m 0755 /opt/morph/web/console
sudo rsync -a ./dist/ /opt/morph/web/console/dist/
```

2. Install and start Console service:

```bash
sudo install -m 0644 ./deploy/systemd/mister-morph-console.service /etc/systemd/system/mister-morph-console.service
sudo systemctl daemon-reload
sudo systemctl enable --now mister-morph-console
```

3. Verify:

```bash
sudo systemctl status --no-pager mister-morph-console
sudo journalctl -u mister-morph-console -f
```

4. Open:

`http://127.0.0.1:9080/console`

Notes:
- Keep `mister-morph` daemon service running; Console reads tasks from `console.endpoints[].url`.
- Ensure endpoint token env vars referenced by `console.endpoints[].auth_token` (via `${ENV_VAR}` syntax) are set for the console process.

## Run mode

The default unit starts `serve` mode on `127.0.0.1:8787`.

- For reverse-proxy setup (Nginx/Caddy), keep the default bind.
- To run Telegram mode, change `ExecStart` in the unit to `... telegram`, then:

```bash
sudo systemctl daemon-reload
sudo systemctl restart mister-morph
```

## systemd sandbox options (what and why)

The unit in `deploy/systemd/mister-morph.service` enables a "least privilege" sandbox profile.
The goal is to reduce blast radius if the process or a tool is abused.

| Option | What it does | Why enabled |
|---|---|---|
| `NoNewPrivileges=yes` | Blocks gaining extra privileges via setuid/setgid binaries. | Prevents privilege escalation paths from child processes. |
| `UMask=0077` | New files are owner-only by default. | Avoids accidental world/group-readable secrets and state files. |
| `PrivateTmp=yes` | Gives the service a private `/tmp`. | Isolates temp files from other services/users. |
| `PrivateDevices=yes` + `DevicePolicy=closed` | Hides host device nodes from the service. | Reduces risk of interacting with sensitive kernel devices. |
| `ProtectSystem=strict` | Mounts most system paths read-only for the service. | Prevents writes to OS directories; limits persistence outside allowed dirs. |
| `ProtectHome=true` | Blocks access to `/home`, `/root`, `/run/user`. | Prevents accidental or malicious reads from user home data. |
| `ProtectControlGroups=true` | Blocks cgroup hierarchy writes. | Prevents tampering with host/service resource control. |
| `ProtectKernelTunables=true` | Blocks writes to `/proc/sys`, `/sys` tunables. | Prevents runtime kernel parameter changes. |
| `ProtectKernelModules=true` | Blocks loading/unloading kernel modules. | Prevents kernel attack surface expansion. |
| `ProtectClock=true` | Blocks changing system clock. | Prevents time tampering that can break logs/tokens/TLS assumptions. |
| `ProtectHostname=true` | Blocks changing hostname/domainname. | Prevents host identity tampering. |
| `ProtectProc=invisible` + `ProcSubset=pid` | Restricts process visibility in `/proc`. | Reduces process metadata leakage across services. |
| `RestrictSUIDSGID=true` | Blocks creating setuid/setgid files. | Prevents creation of privileged executables. |
| `RestrictRealtime=true` | Disables realtime scheduling classes. | Prevents CPU starvation and abuse via realtime priorities. |
| `RestrictNamespaces=true` | Blocks creating most Linux namespaces. | Prevents container-like isolation tricks used in escapes. |
| `LockPersonality=true` | Locks process execution domain/personality. | Reduces obscure ABI-based attack primitives. |
| `SystemCallArchitectures=native` | Allows only native-arch syscalls. | Shrinks syscall surface (no cross-arch ABI paths). |
| `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` | Limits socket families to unix + IPv4/IPv6. | Keeps required networking while blocking uncommon protocol families. |

Related data-isolation settings:

- `StateDirectory=morph` and `CacheDirectory=morph` create dedicated writable directories owned for this service.
- `WorkingDirectory=/var/lib/morph` plus `MISTER_MORPH_FILE_CACHE_DIR` keep writes in predictable locations.
- Guard approvals are stored as a JSON state file under `<file_state_dir>/<guard.dir_name>/approvals/guard_approvals.json` (default under `/var/lib/morph` with this unit layout).

### When to relax sandboxing

Only relax settings when a concrete feature requires it.
Examples:

- If a tool must read a project directory, prefer `BindReadOnlyPaths=` first instead of disabling `ProtectHome`.
- If a workload genuinely needs extra protocol families, expand `RestrictAddressFamilies` minimally.
- Keep `NoNewPrivileges=yes`, `ProtectSystem=strict`, and private state/cache dirs unless you have a strong, documented reason.

## Notes

- The unit already sets `MISTER_MORPH_FILE_CACHE_DIR=/var/cache/morph` and uses a global cache directory.
- `StateDirectory` and `CacheDirectory` are managed by systemd and owned by the service user.
- Review sandboxing directives in `mister-morph.service` before enabling tool features that need broader filesystem access.
