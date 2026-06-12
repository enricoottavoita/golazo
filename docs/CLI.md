# CLI / Agent Mode

Golazo ships with a small set of subcommands that emit JSON to stdout. They are intended for agents (Claude Code, Codex, scripts, CI) that need structured match data without driving the TUI.

The default `golazo` invocation still opens the TUI — the subcommands below are additive.

## When to use Golazo

Use this tool when the user asks about football (soccer) matches. Map their question to the right command:

| User asks about... | Command |
|---|---|
| Matches happening right now | `golazo live` |
| Today's results (already finished) | `golazo finished --days 1` |
| Today's full slate (finished + still-to-come) | `golazo finished --include-upcoming` |
| Results over the last N days (≤7) | `golazo finished --days N` |
| Details for a specific match (events, lineups, stats) | `golazo match <id>` — see [Known limitations](#known-limitations) |
| Which competitions are tracked / what league IDs exist | `golazo leagues` (or `--all`) |

If the user's question doesn't map to one of the above, this tool likely cannot answer it. Golazo does not expose: standings/tables, head-to-head history, individual player stats, transfer news, or fixtures beyond today.

## Quick start (worked example)

The canonical agent flow is **discover → list → drill in**. Every reliable use of `match <id>` first pulls the ID from a list call in the same pipeline:

```bash
# 0. (Optional but recommended) Self-discover the CLI contract
golazo capabilities | jq '.data[0].commands'

# 1. Discover which competitions are active
golazo leagues

# 2. Get today's full slate across active leagues
golazo finished --include-upcoming | jq '.data[] | {
  league: .league.name,
  status,
  home: .home_team.name,
  away: .away_team.name,
  score: (if .home_score != null then "\(.home_score)-\(.away_score)" else null end),
  kickoff_utc: .match_time
}'

# 3. Drill into a specific match by piping its ID
golazo finished --include-upcoming | jq -r '.data[0].id' | xargs golazo match
```

This pattern keeps the in-process page-slug cache populated, which is what makes `match <id>` reliable. See [Known limitations](#known-limitations) for why.

## Subcommands

| Command | Description |
|---|---|
| `golazo live` | Live matches across active leagues |
| `golazo finished [--days N] [--include-upcoming]` | Finished matches over the last N days (1..7, default 1); use `--include-upcoming` to also include today's not-yet-started matches |
| `golazo match <id>` | Full match details (events, lineups, stats) |
| `golazo leagues [--all]` | Active leagues (or every supported league) |
| `golazo capabilities` | Machine-readable contract describing every subcommand, flag, error code and env var — call this once at session start to self-discover the CLI |

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

## Data schema

Every command's `data` array contains one of three object shapes. All field names are stable across calls. Fields marked `null when ...` are present but null in those states — agents should always nil-check.

### `Match` (returned by `live`, `finished`)

```yaml
id:          int        # FotMob match ID — pass to `golazo match`
league:
  id:        int
  name:      string     # e.g. "Premier League"
  country:   string     # e.g. "England", "International", "Europe"
home_team:
  id:        int
  name:      string     # full name, e.g. "Manchester United"
  short_name: string    # abbreviated, e.g. "Man Utd"
away_team:   { same shape as home_team }
status:      string     # one of: "live" | "finished" | "not_started" | "postponed" | "cancelled"
home_score:  int|null   # null when status == "not_started"
away_score:  int|null   # null when status == "not_started"
match_time:  string     # RFC3339 timestamp in UTC, e.g. "2026-06-12T19:00:00Z"
live_time:   string|null # null unless status == "live"; e.g. "45+2", "HT", "67'"
round:       string     # e.g. "Matchday 17", "Round of 16"
page_url:    string     # FotMob page slug (internal use; agents can ignore)
```

### `MatchDetails` (returned by `match`)

`MatchDetails` embeds every `Match` field above and adds:

```yaml
events:
  - id:             int
    minute:         int          # base minute, e.g. 45
    display_minute: string       # formatted, e.g. "45+2'"
    type:           string       # "goal" | "card" | "substitution"
    team:           Team
    player:         string|null
    assist:         string|null
    event_type:     string|null  # "yellow" | "red" | "in" | "out"
    own_goal:       bool|null
    timestamp:      string       # RFC3339
home_lineup:        []string     # player names (legacy; prefer home_starting)
away_lineup:        []string
home_starting:                   # full lineup detail
  - { id, name, number, position, rating }
away_starting:      [...]
home_substitutes:   [...]
away_substitutes:   [...]
home_formation:     string       # e.g. "4-3-3"
away_formation:     string
home_score / away_score:         # final score (always set for finished)
half_time_score:    { home, away } | absent
penalties:          { home, away } | absent
venue:              string
referee:            string
attendance:         int
match_duration:     int          # 90, 120
extra_time:         bool
statistics:                       # possession, shots, etc.
  - { key, label, home_value, away_value }
home_xg / away_xg:  number|absent
highlight:                        # YouTube/source link if available
  { url, image, source, title } | absent
aggregate_score:    string       # two-legged ties only, e.g. "5 - 7"
who_lost_on_aggregate: string    # team name eliminated
winner:             "home"|"away"|null
```

### `League` (returned by `leagues`)

```yaml
id:           int
name:         string
country:      string
country_code: string   # often empty for international competitions
```

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

### Basic invocations

```bash
# Live matches, compact JSON
golazo live

# Finished matches over the last 3 days, indented
golazo finished --days 3 --pretty

# Today's full slate (finished + still-to-come)
golazo finished --include-upcoming

# Single match details (use an ID from a prior list call)
golazo finished | jq -r '.data[0].id' | xargs golazo match

# Discover league IDs to interpret results
golazo leagues --all

# Agent mode + offline safety in CI
GOLAZO_AGENT=1 GOLAZO_OFFLINE=1 golazo live --mock
```

### jq recipes

Most agent flows pipe Golazo's output through `jq`. These are the patterns worth memorizing:

```bash
# One-line live score summary
golazo live | jq -r '.data[] | "\(.home_team.name) \(.home_score)-\(.away_score) \(.away_team.name) [\(.live_time)]"'

# Filter by league name (case-insensitive match)
golazo finished --include-upcoming \
  | jq '[.data[] | select(.league.name | test("World Cup"; "i"))]'

# Filter by status (e.g. only what's still to come)
golazo finished --include-upcoming \
  | jq '[.data[] | select(.status == "not_started")]'

# Extract just IDs, ready for chaining to `match`
golazo finished --days 3 | jq -r '.data[].id'

# Check whether the result was degraded (partial-failure-aware retry decision)
golazo finished --days 7 | jq '{ok: ((.degraded // false) | not), failed: (.failed_dates // [])}'

# Goal events only, ordered by minute
golazo finished | jq -r '.data[0].id' | xargs golazo match \
  | jq '.data[0].events | map(select(.type == "goal")) | sort_by(.minute)'
```

## Notes

- Stdout receives **only** the JSON envelope. All logs go to stderr — safe to pipe through `jq`.
- List output is sorted deterministically (`match_time` then `id`) so repeated invocations diff cleanly.
- The TUI experience is unchanged — no flags here alter the interactive default.

## Known limitations

### `match <id>` requires IDs from a prior list call

FotMob's match-details endpoint is gated behind Cloudflare Turnstile when called directly. Golazo's primary fetch path retrieves details by parsing the match's page HTML using a slug that is only populated when the match appears in a list response (`live` or `finished`). Calling `golazo match <id>` against an ID that has not previously been seen in the current process will most likely return an `upstream_error` (HTTP 404 from the API fallback).

**Recommended agent flow**: list first, then drill in within the same shell pipeline or session:

```bash
# Pick an ID from the list, then fetch its details
golazo finished --days 1 | jq -r '.data[0].id' | xargs golazo match
```

The slug cache lives in process memory and does not persist across invocations, so a fresh `golazo match <id>` cold call is not reliable. Agents should treat `match` as a follow-up step to `live` / `finished`, not a standalone lookup.

### Debug logging is sparse on list endpoints

`--debug` and `GOLAZO_AGENT=1` only emit logs at the FotMob client's match-details fetch path. The list endpoints (`live`, `finished`, `leagues`) are largely silent. This is by design — agents are expected to interpret the JSON envelope (including `degraded` / `failed_dates`), not stderr logs.
