# AGENTS.md — plugin-doctor

Standalone plugin repo owning the externalized `charly doctor` command
(`command:doctor`, compiled-in) plus the kernel's `verb:freshness-guard`
preflight hook. The plugin is a Go module at `candy/plugin-doctor/` (module path
`github.com/opencharly/plugin-doctor/candy/plugin-doctor`); the root `charly.yml`
declares `discover: candy` (so the repo is a project and its candy is scanned)
plus the `check-doctor-local` R10 witness bed.

Canonical files:

- `candy/plugin-doctor/charly.yml` — the `plugin-doctor:` candy entity
  (`plugin:` block, `plan:` check).
- `candy/plugin-doctor/plugin.go` / `provider.go` — `NewProvider()` /
  `NewMeta()` / `CliMain` and the `Invoke(OpRun)` surface.
- `candy/plugin-doctor/command.go` — the `charly doctor` CLI + the check-list
  group orchestration.
- `candy/plugin-doctor/freshness.go` — the ported `verb:freshness-guard`
  preflight logic.
- `candy/plugin-doctor/hostfacts.go` — the peer `verb:gpu` / `verb:credential`
  `InvokeProvider` dispatches.
- `candy/plugin-doctor/data.go` / `data.yml` — the embedded install-hint /
  device / distro tables.
- `candy/plugin-doctor/schema/doctor.cue` — the self-contained `#DoctorPlugin`.
- `charly.yml` — the root project manifest. Its top-level keys are `discover:`
  (the `candy` scan) plus the `check-doctor-app` template and the
  `check-doctor-local` disposable local bed (the R10 witness).
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.
- `README.md` — user overview only; never agent guidance.

## Load these skills first (R0)

- `/charly-core:charly-doctor` — the host-dependency checker and hardware
  detector for `charly doctor`. Load before changing the check list or report.
  This candy carries no `skill:` entity of its own; the gap is tracked in
  [opencharly/opencharly#291](https://github.com/opencharly/opencharly/issues/291).
- `/charly-internals:plugin` — the plugin authoring reference: the `plugin:`
  block, the `command` + `verb` provider classes, the per-plugin CUE-schema
  contract.
- `/charly-internals:git-workflow` — before any git/PR action.

## Build / validate / test

- `go build ./...` in `candy/plugin-doctor/` — compile the plugin module.
- `go test ./...` in `candy/plugin-doctor/` — the plugin's Go tests (the command,
  data, distro, freshness, and schema-serve seams).
- `charly box validate` at the repo root — the structural check (the candy +
  `plugin:` block, CUE schema).
- The merge gate is the **org-wide** `charly/pr-validator` (required check
  `validate / validate`, defined in `opencharly/.github`); this repo has **no**
  per-repo candy gate.
- The R10 witness is the `check-doctor-local` disposable local bed (declared in
  `charly.yml`): `charly doctor` exits 0 and prints the host-dependency report.

## Modify this repo

- Edit the `plugin-doctor:` candy entity, the Go source, and
  `schema/doctor.cue` **together** — the schema is the served declaration
  surface.
- Keep doctor COMPILED-IN: its `Invoke(OpRun)` needs the in-proc reverse channel
  for the peer `verb:gpu` / `verb:credential` dispatches. Reach those peers over
  `sdk.Executor.InvokeProvider` — never re-introduce a hidden core-command
  forward or a core-owned host-probe seam.
- The GPU/VFIO/device detection primitives and the credential-store health probe
  are NOT a core dependency: this plugin reaches `candy/plugin-gpu`'s `verb:gpu`
  and `candy/plugin-secrets`' `verb:credential` PEER-TO-PEER over
  `sdk.Executor.InvokeProvider`, and the install-hint / distro-family /
  device-description / device-pattern tables are this plugin's own embed
  (`data.go` / `data.yml`). The out-of-process `CliMain` path passes a nil
  executor, so those two peer calls degrade to zero values (the report still
  renders, minus the two peer-plugin-backed sections); the canonical placement
  stays compiled-in.

## Landing

- PR-only. Every change lands through a pull request; the org-required
  `charly/pr-validator` validates the diff and body and arms native auto-merge on
  PASS. Direct pushes to `main` are blocked.
- History lives in `CHANGELOG/` (written by `tag-on-merge` at merge time); the PR
  body IS the changelog.
- The authoritative rulebook is the umbrella `AGENTS.md` in
  `opencharly/opencharly` and `charly/AGENTS.md` in the charly repo. Do not
  restate its rules here.
