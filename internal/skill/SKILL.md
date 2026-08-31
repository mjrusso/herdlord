---
name: herdlord
description: "Inspect Herdr coding agents and manage configured local and remote targets with the Herdlord CLI. Use when the user asks which agents are working, blocked, done, or idle; asks whether Herdr targets are reachable or compatible; wants recent output from a known Herdr pane; or explicitly asks to change Herdlord target configuration. Herdlord never sends prompts or input to agents."
---

# Herdlord

Use Herdlord to inspect Herdr agents across the user's configured targets.
Herdlord polls each target independently and does not send input to agents.

## Inspect the installed command surface

The installed binary defines the syntax for its version. Check help before an
unfamiliar operation:

```bash
herdlord --help
herdlord list --help
herdlord status --help
herdlord read --help
herdlord targets --help
```

Use JSON when consuming results programmatically. Do not parse the text tables.

## Inspect the fleet

List agents across active targets:

```bash
herdlord list --output json
```

Use `--target <name>` to restrict the request. Add `--include-paused` only when
the user needs paused targets included. A target failure is recorded on that
target; it does not hide successful results from other targets.

Agent states have distinct meanings:

- `blocked` means Herdr recognized a question or approval prompt.
- `done` means unseen background work finished.
- `working` means the agent is active.
- `idle` means no work is currently detected.
- `unknown` means Herdr cannot classify the agent confidently.

Treat `unknown` as unknown, not as finished. Prioritize `blocked`, then `done`,
when the user asks which agents need attention.

Agent records include Herdr's workspace and tab labels and their stable
`workspace_id` and `tab_id`. Use those labels instead of deriving project names
from `cwd`.

## Diagnose targets

Inspect every configured target or one named target:

```bash
herdlord status --output json
herdlord status <target> --output json
herdlord targets list --output json
```

Keep these states distinct:

- `paused` is an intentional local choice.
- `backing off` means Herdlord will retry after a failure.
- `unreachable` means the target command failed or timed out.
- `no herdr` means the target ran commands but Herdr was not found.
- `skewed` means the target's Herdr protocol is too old.
- `newer` means the target uses a newer, untested protocol; Herdlord still
  attempts to list agents and read their output.

Target status records include `lastSuccess`, the most recent successful
refresh. Use it with the current state and error when explaining an outage.
This is Herdlord connection state, not Herdr agent attention state.

Do not recommend transport-specific fixes unless the target prefix identifies
the transport and its error supports that conclusion.

## Manage targets

Inspect configuration before changing it:

```bash
herdlord targets list --output json
herdlord targets show <name> --output json
```

Only change target configuration when the user asks. The available mutations
are:

```bash
herdlord targets add <name> --prefix <prefix>
herdlord targets update <name> --prefix <prefix>
herdlord targets update <name> --attach-prefix <prefix>
herdlord targets pause <name>
herdlord targets resume <name>
herdlord targets rm <name>
```

Omitting `--prefix` on `targets add` creates a local target. The attach prefix
defaults to the normal prefix. An unhealthy result from add or update is saved;
report both the successful configuration change and the target state.

## Read recent agent output

Use the target name and `pane_id` returned by `herdlord list`:

```bash
herdlord read <target> <pane-id> --lines 50
```

The output is plain text. Reading does not mark a completed Herdr pane as seen.
Do not infer permission to attach, send input, approve prompts, or otherwise
operate an agent from permission to inspect it.

## Safety

- Fleet inspection and agent output commands are observational.
- Report partial failures alongside successful target results.
- Use `herdlord targets` commands for requested configuration changes. Do not
  edit the targets file directly.
- Do not turn target prefixes into shell commands or reinterpret their argv.
