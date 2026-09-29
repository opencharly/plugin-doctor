# plugin-doctor

The `charly doctor` host-dependency-status surface for OpenCharly — the
externalized command plugin (`command:doctor`, compiled-in) plus the kernel's
`verb:freshness-guard` preflight hook.

The plugin owns the command end to end: the flag grammar (`--json`), the entire
check list + group orchestration (container engine / build infra / service
management / VMs / VFIO / encrypted storage / secret storage / tunnels / merge &
registry / shell & TTY / podman machine), the pass/warn/fail verdicts, the human
+ JSON report formatting, the exit code, AND the pure host ops it runs itself
(binary probes, file reads).

## What it provides

| Capability | Surface |
|---|---|
| `command:doctor` | the `charly doctor` CLI |
| `verb:freshness-guard` | the kernel's preflight-phase freshness check |

## How to use it

The command is compiled in — no candy composition is needed:

```bash
charly doctor
charly doctor --json
```

`charly doctor` exits 0 and prints the host-dependency report (or exits non-zero
when a required dependency is missing); `--json` emits the same report as JSON.

## Layout

- `candy/plugin-doctor/` — the plugin module: `plugin.go` (the provider +
  `NewProvider()` / `NewMeta()` / `CliMain`), `provider.go` (the `Invoke(OpRun)`
  path), `command.go` (the CLI + check orchestration), `freshness.go` (the
  ported `verb:freshness-guard` logic), `hostfacts.go` (the peer-plugin
  dispatches), `data.go` / `data.yml` (the embedded tables), `schema/doctor.cue`
  (the self-contained `#DoctorPlugin`), the Go tests, `cmd/serve/main.go`.
- `charly.yml` — the root project manifest (`discover: candy`) plus the
  `check-doctor-local` disposable local bed.
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.

## Related

- Owning skill: `/charly-core:charly-doctor` — the host-dependency checker and
  hardware detector for `charly doctor`. This candy carries no `skill:` entity of
  its own; the gap is tracked in
  [opencharly/opencharly#291](https://github.com/opencharly/opencharly/issues/291).
- `/charly-internals:plugin` — the plugin/provider model, including the `command`
  and `verb` provider classes.
- [`opencharly/charly`](https://github.com/opencharly/charly) — the charly CLI.
