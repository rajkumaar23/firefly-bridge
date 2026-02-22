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