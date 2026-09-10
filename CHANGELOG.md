# Changelog

## Unreleased

## v0.2.0 - 2026-09-10

- Add a pasture dashboard view that shows each target as a fenced pen and each
  agent as an animated sheep. Press `v` to switch between the table and the
  pasture.
- Draw the pasture with Kitty graphics in Kitty and Ghostty, and with an ASCII
  renderer in other terminals. Press `R` to switch renderers. Set
  `HERDLORD_RENDERER` or `HERDLORD_KITTY` to override the automatic choice.
- Add `herdlord demo` to open the dashboard with simulated targets and agents.
- Add an activity row that reports recent target and agent changes.
- Read the visible screen when an agent is working and Herdr cannot capture its
  alternate-screen history, instead of reporting a read error.

## v0.1.2 - 2026-09-08

- Support Herdr 0.9.0 and protocol 22.
- Present target health in a separate dashboard panel and improve target error
  notices.
- Keep the agent inspector usable in shorter terminals.
- Remove the redundant dashboard header.

## v0.1.1 - 2026-09-01

- Support SSH targets that use Fish as their remote login shell, including
  Herdr installations in `~/.local/bin`.
- Render keyboard shortcuts consistently throughout the TUI.

## v0.1.0 - 2026-08-31

- Add a terminal dashboard that combines agents from local and remote Herdr
  sessions and puts agents that need attention first.
- Add dashboard controls for inspecting agent output and attaching to Herdr
  sessions.
- Add text and JSON commands for monitoring agents and managing targets.
- Add target management through the dashboard and CLI, with automatic reloads.
- Add `herdlord skill` with version-matched instructions for coding agents.
