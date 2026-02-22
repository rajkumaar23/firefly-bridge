# Firefly Bridge

A semi-automatic bridge between banks, brokerages, and [Firefly III](https://firefly-iii.org/).

Firefly Bridge fetches transactions and balances directly from financial institutions using browser automation and CSV exports, then imports them into [Firefly III](https://firefly-iii.org/) in a deterministic and repeatable way. The goal is not to provide a universal, plug-and-play solution, but a transparent and customizable pipeline that can be adapted to any institution or account structure.

All institution-specific logic — login flows, CSS selectors, CSV column mappings, and secret references — is defined in a `config.yaml` file, keeping sensitive details private and configuration explicit. See [CONFIG-DSL.md](CONFIG-DSL.md) for a complete reference of every available option.

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