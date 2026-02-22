# Firefly Bridge

A semi-automatic bridge between banks, brokerages, and [Firefly III](https://firefly-iii.org/).

Firefly Bridge fetches transactions and balances directly from financial institutions using browser automation and CSV exports, then imports them into [Firefly III](https://firefly-iii.org/) in a deterministic and repeatable way. The goal is not to provide a universal, plug-and-play solution, but a transparent and customizable pipeline that can be adapted to any institution or account structure.

All institution-specific logic — login flows, CSS selectors, CSV column mappings, and secret references — is defined in a `config.yaml` file, keeping sensitive details private and configuration explicit. See [CONFIG-DSL.md](CONFIG-DSL.md) for a complete reference of every available option.

> [!CAUTION]
> **Back up your Firefly III database before running any tool in this project.**
>
> `firefly-bridge`, `backfill-hashes`, and `portfolio-sync` all write directly to your Firefly III instance via its API. Mistakes in configuration or unexpected data can result in incorrect or duplicate transactions that are difficult to reverse. Export a database snapshot before each run, and verify the results on a test instance first if possible.

### How firefly-bridge tracks state in Firefly

firefly-bridge relies on two Firefly III fields to operate correctly and avoid duplicating data:

- **`internal_reference` (transactions)** — used for regular accounts. Every transaction imported by firefly-bridge is tagged with a deterministic SHA-256 hash stored in this field. Before importing a transaction, firefly-bridge searches Firefly for a matching `internal_reference` and skips the transaction if one is found. **Do not manually clear or overwrite this field** on transactions managed by firefly-bridge.
- **Account notes (investment accounts)** — used to store the current holdings of an investment account (e.g. `AAPL=10.00000000,VTSAX=50.00000000`). `portfolio-sync` reads this field to calculate real-time portfolio value. **Do not use the notes field for other purposes** on accounts managed by firefly-bridge.

> [!WARNING]
> **Run `backfill-hashes` before your first firefly-bridge sync if you already have transactions in Firefly.**
>
> firefly-bridge uses the `internal_reference` field on each transaction to detect duplicates. If your Firefly database already contains transactions that were imported manually or by another tool, those transactions will lack an `internal_reference` and firefly-bridge will re-import them as duplicates on its first run.
>
> The `backfill-hashes` companion tool walks every asset account, computes the correct hash for each existing transaction, and writes it to `internal_reference` — preventing duplication without touching any other transaction data. Run it once before enabling firefly-bridge:
>
> ```
> backfill-hashes --host http://firefly.example.com --token <token>
> ```
>
> The tool will show you exactly how many transactions and splits will be updated for each account and ask for your confirmation before making any changes.

## Backfill Hashes

`backfill-hashes` is a one-time setup tool that populates the `internal_reference` field on existing Firefly transactions so that firefly-bridge can identify them as already-imported and will not create duplicates.

### Usage

```
backfill-hashes [flags]
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--host` | string | `""` | Firefly host URL. Can also be set via `$FIREFLY_HOST`. |
| `--token` | string | `""` | Firefly personal access token. Can also be set via `$FIREFLY_TOKEN`. |
| `--debug` | bool | `false` | Enable verbose debug logging. |

For each asset account the tool will display the number of transaction groups and individual splits that are missing `internal_reference`, then ask:

```
[Checking Account] 42 transaction group(s) | 42 split(s) missing internal_reference
Update? (y/n):
```

Answer `y` to apply the hashes for that account or `n` to skip it. The tool tracks which transaction groups have already been updated, so a single group that appears in multiple accounts' transaction lists (e.g. a transfer) is only updated once.

## CLI Flags

```
firefly-bridge [flags]
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `-config` | string | `"config.yaml"` | Path to the YAML configuration file. |
| `-state-file` | string | `".firefly-bridge-state.json"` | Path to the state file that tracks the last successful run per institution and account. |
| `-institution` | string | `""` | Run only the institution with this exact name (case-sensitive). Skips all other institutions and also bypasses cooldown and balance-unchanged checks for the specified institution. |
| `-force` | bool | `false` | Bypass the per-institution cooldown and the per-account balance-unchanged skip. Forces a full sync of every institution and account regardless of state. |
| `-sync-days` | int | `10` | Force a full transaction sync for an account after this many days have elapsed since the last sync, even if the scraped balance matches the Firefly balance. |
| `-debug` | bool | `false` | Enable verbose debug logging for firefly-bridge internals. |
| `-cdp-debug` | bool | `false` | Enable verbose debug logging for browser automation. Useful for diagnosing selector issues. |
| `-csv-debug` | bool | `false` | Log every parsed CSV/Excel row with its row number. Useful for diagnosing `skip_head_rows`, `skip_tail_rows`, and column index issues. |

### Runtime directories

Two directories are created automatically alongside the state file at startup:

- `downloads/` — temporary landing zone for CSV/Excel files downloaded during browser automation; files are read and then deleted after each sync.
- `chromedp-data/` — browser user data directory used by the automation session (cookies, cache, local storage). 

## Portfolio Sync

`portfolio-sync` is a companion service that fetches current market prices for the securities stored in your Firefly investment account notes and creates Profit/Loss transactions in Firefly III. Unlike `firefly-bridge` (which requires a browser and user interaction to download statements), `portfolio-sync` is fully headless and designed to run on a schedule.

### Running as a cron job with Docker Compose

The recommended way to run `portfolio-sync` continuously is alongside [Ofelia](https://github.com/mcuadros/ofelia), a Docker-native job scheduler. The container sleeps indefinitely and Ofelia exec's the binary on your schedule — no separate cron daemon or host access required.

```yaml
services:
  portfolio-sync:
    image: ghcr.io/rajkumaar23/firefly-portfolio-sync:latest
    env_file:
      - stack.env
    entrypoint: ["/bin/sh", "-c", "sleep infinity"]
    restart: unless-stopped
    environment:
      - FIREFLY_HOST=${FIREFLY_HOST}
      - FIREFLY_TOKEN=${FIREFLY_TOKEN}
      - TZ=America/Los_Angeles
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.portfolio-sync.schedule: "0 20 13 * * *"
      ofelia.job-exec.portfolio-sync.command: "/usr/local/bin/portfolio-sync"

  ofelia:
    image: mcuadros/ofelia:latest
    restart: unless-stopped
    command: daemon --docker
    environment:
      - TZ=America/Los_Angeles
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    depends_on:
      - portfolio-sync
```

The schedule uses a 6-field cron expression (seconds first): `0 20 13 * * *` runs daily at 13:20. Adjust to match your preferred sync time and timezone.

---