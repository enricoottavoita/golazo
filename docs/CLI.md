# CLI / Agent Mode

Golazo ships with a small set of subcommands that emit JSON to stdout. They are intended for agents (Claude Code, scripts, CI) that need structured match data without driving the TUI.

The default `golazo` invocation still opens the TUI — the subcommands below are additive.

## Subcommands

| Command | Description |
|---|---|
| `golazo live` | Live matches across active leagues |
| `golazo finished [--days N]` | Finished matches over the last N days (1..7, default 1) |
| `golazo match <id>` | Full match details (events, lineups, stats) |
| `golazo leagues [--all]` | Active leagues (or every supported league) |

### Common flags

| Flag | Description |
|---|---|
| `--mock` | Use bundled mock data, no network |
| `--debug` | Emit debug logs to stderr |
| `--timeout <dur>` | Overall request timeout (default `15s`) |
| `--pretty` | Indent JSON output |

## JSON contract

### Success envelope

```json
{
  "status": "ok",
  "count": 2,
  "data": [ ... ]
}
```

Single-item responses (e.g. `match <id>`) still use a `data` array with `count: 1`.

### Degraded envelope

`finished` over multiple days may partially fail. When at least one day succeeds, the envelope is flagged degraded with the failing dates listed:

```json
{
  "status": "ok",
  "degraded": true,
  "failed_dates": ["2026-06-10"],
  "count": 12,
  "data": [ ... ]
}
```

### Error envelope

Errors go to **stderr**, stdout stays empty:

```json
{
  "status": "error",
  "code": "not_found",
  "message": "no match found for id 99999999"
}
```

Error codes: `invalid_args`, `not_found`, `upstream_error`, `timeout`, `offline`.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Upstream / unknown error |
| `2` | Invalid arguments |
| `3` | Not found |
| `4` | Timeout |
| `5` | Offline (network disabled via env) |

## Environment variables

| Var | Effect |
|---|---|
| `GOLAZO_AGENT=1` | Forces compact JSON, enables stderr debug logging |
| `GOLAZO_OFFLINE=1` | Refuses any network call; subcommands return `offline` unless `--mock` is set |

## Examples

```bash
# Live matches, compact JSON
golazo live

# Finished matches over the last 3 days, indented
golazo finished --days 3 --pretty

# Single match details
golazo match 4506424

# Discover league IDs to interpret results
golazo leagues --all

# Pipe through jq
golazo live | jq '.data[] | {home: .home_team.name, away: .away_team.name, score: "\(.home_score)-\(.away_score)"}'

# Agent mode + offline safety in CI
GOLAZO_AGENT=1 GOLAZO_OFFLINE=1 golazo live --mock
```

## Notes

- Stdout receives **only** the JSON envelope. All logs go to stderr — safe to pipe through `jq`.
- List output is sorted deterministically (`match_time` then `id`) so repeated invocations diff cleanly.
- The TUI experience is unchanged — no flags here alter the interactive default.
