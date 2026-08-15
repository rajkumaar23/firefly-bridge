# Firefly Bridge Configuration DSL Reference

This document is a complete reference for every attribute and feature available in the Firefly Bridge configuration DSL. Configuration is written in YAML and controls how the bridge authenticates with financial institutions, scrapes account data, and syncs it to your Firefly III instance.

---

## Table of Contents

- [Config File Basics](#config-file-basics)
  - [Environment Variable Expansion](#environment-variable-expansion)
  - [File Imports (`!import`)](#file-imports-import)
- [Root Structure](#root-structure)
- [`firefly`](#firefly)
- [`secrets`](#secrets)
  - [`secrets.onepassword`](#secretsonepassword)
  - [`secrets.bitwarden`](#secretsbitwarden)
  - [Secret References in Values](#secret-references-in-values)
- [`ai`](#ai)
- [`browser_exec_path`](#browser_exec_path)
- [`institutions`](#institutions)
  - [Institution Fields](#institution-fields)
  - [`accounts`](#accounts)
    - [Account Fields](#account-fields)
    - [Account Type: `regular`](#account-type-regular)
    - [Account Type: `investment`](#account-type-investment)
- [`vendors`](#vendors)
- [Browser Step Reference](#browser-step-reference)
  - [`navigate`](#navigate)
  - [`wait_visible`](#wait_visible)
  - [`wait_not_visible`](#wait_not_visible)
  - [`wait_not_present`](#wait_not_present)
  - [`click`](#click)
  - [`sleep`](#sleep)
  - [`reload`](#reload)
  - [`send_keys`](#send_keys)
  - [`set_value`](#set_value)
  - [`balance`](#balance)
  - [`orders`](#orders)
  - [`transactions`](#transactions)
    - [CSV Mode](#csv-mode)
    - [Excel Mode](#excel-mode)
    - [`options`](#options)
    - [`fields`](#fields)
    - [Amount Columns](#amount-columns)
    - [`skip_row_conditions` / `negate_if`](#skip_row_conditions--negate_if)
  - [`holdings`](#holdings)
  - [`evaluate`](#evaluate)
  - [`skip_remaining_if`](#skip_remaining_if)
- [Template Functions](#template-functions)
- [Amount Parsing](#amount-parsing)
- [Validation Rules Summary](#validation-rules-summary)
- [Full Example](#full-example) → [`config.example.yaml`](config.example.yaml)

---

## Config File Basics

The main config file is YAML. By default, the bridge looks for `config.yaml` in the current directory; override with `-config <path>`.

### Environment Variable Expansion

Use `${ENV:KEY}` anywhere in the config to substitute an environment variable:

```yaml
firefly:
  host: "${ENV:FIREFLY_HOST}"
  token: "${ENV:FIREFLY_TOKEN}"
browser_exec_path: "${ENV:BROWSER_PATH}"
```

The `ENV:` prefix makes the syntax unambiguous — it never conflicts with JavaScript
template literals (`${expr}`), CSS selectors, or any other context where `$` has
meaning. Plain `$` and `${...}` without the prefix are left untouched.

### File Imports (`!import`)

Within any YAML sequence, individual items can be replaced by the contents of an external file using the `!import` tag. Paths are resolved relative to the directory of the main config file.

```yaml
# Import a single institution from its own file
institutions:
  - !import "chase.yaml"
  - !import "bank-of-america.yaml"

# Or import the entire list from one file
institutions: !import "institutions.yaml"
```

The referenced file must contain a valid YAML value for that position in the sequence (e.g., a mapping for a single institution, or a sequence for the full `institutions` list).

---

## Root Structure

```yaml
firefly:          # required
  host: "..."
  token: "..."

secrets:          # optional
  onepassword:
    token: "..."
  bitwarden:
    session: "..."

ai:               # optional
  enabled: true
  base_url: "..."
  model: "..."

browser_exec_path: "/path/to/chrome"  # required

institutions:     # required, minimum 1
  - name: "..."
    login: [...]
    logout: [...]  # optional
    accounts: [...]

vendors:          # optional
  - name: "..."
    match: "..."
    login: [...]
    orders: [...]
```

| Field | Type | Required | Description |
|---|---|---|---|
| `firefly` | object | yes | Firefly III API connection settings |
| `secrets` | object | no | Secret provider configuration |
| `ai` | object | no | AI-assisted category/budget assignment |
| `browser_exec_path` | string | yes | Absolute path to the browser executable used for automation |
| `institutions` | array | yes (min 1) | List of financial institutions to sync |
| `vendors` | array | no | Merchants whose order history is scraped on demand to categorize ambiguous charges |

---

## `firefly`

Connection settings for your Firefly III instance.

```yaml
firefly:
  host: "https://firefly.example.com"
  token: "eyJ..."
```

| Field | Type | Required | Validation | Description |
|---|---|---|---|---|
| `host` | string | yes | valid HTTP/HTTPS URL | Base URL of your Firefly III instance. Trailing slash is stripped automatically. |
| `token` | string | yes | valid JWT format | Personal Access Token from **Firefly III → Profile → OAuth → Personal Access Tokens**. |

---

## `secrets`

Optional configuration for secret providers. When configured, `value` fields in [`send_keys`](#send_keys) and [`set_value`](#set_value) steps can reference secrets by URI instead of hardcoding credentials. You can configure either or both providers; each is identified by its URI scheme (`op://` for 1Password, `bw://` for Bitwarden).

```yaml
secrets:
  onepassword:
    token: "ops_..."
  bitwarden:
    session: "${ENV:BW_SESSION}"
```

### `secrets.onepassword`

Configures the 1Password secret provider using a Service Account token. The token is verified at startup with a single authenticated call, so an invalid or expired token fails fast rather than mid-sync.

| Field | Type | Required | Description |
|---|---|---|---|
| `token` | string | yes | 1Password Service Account token (create one in **1Password → Developer → Service Accounts**) |

### `secrets.bitwarden`

Configures the Bitwarden Password Manager provider. Because Bitwarden's Go SDK only covers Secrets Manager, firefly-bridge reads structured login items (username/password/custom fields) through the official [`bw` CLI](https://bitwarden.com/help/cli/) — so the CLI must be installed and its vault **unlocked** before a sync.

**One-time setup:**

```bash
bw login                      # authenticate (email/password or API key)
export BW_SESSION=$(bw unlock --raw)   # unlock and capture the session key
```

Then reference `${ENV:BW_SESSION}` from the config (or let the provider pick up the ambient `BW_SESSION` automatically by omitting `session`). The provider verifies at startup that the `bw` CLI is installed and the vault is unlocked, failing fast with an actionable message if it is missing, locked, or logged out.

| Field | Type | Required | Description |
|---|---|---|---|
| `session` | string | no | Vault unlock key from `bw unlock`. When omitted, the ambient `BW_SESSION` environment variable is used. Supports `${ENV:...}` expansion. |
| `server_url` | string | no | Points the CLI at a non-default server — Bitwarden EU (`https://vault.bitwarden.eu`), a self-hosted instance, or Vaultwarden. Defaults to Bitwarden US cloud. Applied at startup via `bw config server`; ignored if the CLI is already logged in to a server. |
| `appdata_dir` | string | no | Overrides the CLI data directory (`BITWARDENCLI_APPDATA_DIR`) to isolate firefly-bridge's Bitwarden state from your interactive `bw` sessions. |
| `bw_path` | string | no | Path to the `bw` binary. Defaults to `bw` on `PATH`. |

### Secret References in Values

Once a secret provider is configured, secrets can be referenced in two ways depending on the step type:

**Whole-value reference** — `value` fields in `send_keys` and `set_value` steps accept a bare URI that is resolved to the secret value:

```yaml
- type: send_keys
  selector: "#username"
  value: "op://vault-name/item-name/field-name"
```

If the value does not contain `://`, it is used as a literal string. Secret resolution happens before template parsing, so a value cannot be both a secret reference and a template expression.

**Inline reference** — `evaluate` fields in `balance` and `evaluate` steps support `op://` and `bw://` URIs embedded anywhere inside a larger string (e.g., a JavaScript snippet). Each reference is resolved independently, and the surrounding text is left untouched:

```yaml
- type: evaluate
  evaluate: |
    fetch('https://api.example.com/token', {
      body: JSON.stringify({
        username: 'op://Vault/Item/username',
        password: 'bw://example-bank/password',
      }),
    })
```

**1Password URI format:** `op://vault/item/field`

- `vault` — name or UUID of the 1Password vault
- `item` — name or UUID of the item
- `field` — name of the field within the item (e.g., `username`, `password`)

**Bitwarden URI format:** `bw://item/field`

- `item` — name (matched by the CLI's search) or ID of the vault item
- `field` — a built-in login field (`username`, `password`, `totp`, `notes`, `uri`) or the name of a custom field on the item (matched case-insensitively)

```yaml
- type: send_keys
  selector: "#username"
  value: "bw://example-bank/username"
- type: send_keys
  selector: "#password"
  value: "bw://example-bank/password"
```

---

## `ai`

Optional. Enables AI-assisted assignment of a **category** and/or **budget** to each new transaction just before it is uploaded to Firefly III. Any endpoint that speaks the OpenAI chat-completions schema works — `llama.cpp`'s server, Ollama's OpenAI shim, LocalAI, OpenAI itself, etc. — so it can run entirely against a small self-hosted model.

```yaml
ai:
  enabled: true
  base_url: "http://jetson.local:8080/v1"
  api_key: "op://my-vault/openai/token"   # optional
  model: "qwen2.5:9b"
  categories: true
  budgets: false
  overwrite_existing: false
  always_ask_model: false
  max_examples: 5
  timeout_seconds: 30
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | no | `false` | Master switch. When absent or `false`, transactions are uploaded unchanged. |
| `base_url` | string | if enabled | — | OpenAI-compatible API root, including the version segment (e.g. `.../v1`). Requests go to `{base_url}/chat/completions`. |
| `api_key` | string | no | — | Sent as a `Bearer` token. Supports `${ENV:...}` expansion and `op://` secret references. Omit for auth-less local servers. |
| `model` | string | if enabled | — | Model name passed to the endpoint. |
| `categories` | bool | no | `false` | Let the model assign a category. |
| `budgets` | bool | no | `false` | Let the model assign a budget. |
| `overwrite_existing` | bool | no | `false` | Enrich even when the transaction already has a category/budget (e.g. a noisy label from the source CSV), re-mapping it onto Firefly's taxonomy. An existing value is only ever replaced with a better one that exists in Firefly — never blanked out. |
| `always_ask_model` | bool | no | `false` | Disable the reuse-first shortcut so the model is consulted for every wanted field even when similar past transactions agree. The historical values are still passed to the model as few-shot context. Enable when you don't trust historical labels; note it bypasses the "mirror existing rule assignments" behavior. |
| `split_orders` | bool | no | `true` | When a matched vendor order's `line_items` fall into different categories, upload the charge as a Firefly **split transaction** with one split per category. Set to `false` to categorize such charges as a single transaction. Only ever applies to vendors whose [`orders`](#orders) step emits `line_items`. |
| `max_examples` | int | no | `5` | How many similar past transactions are used as reuse precedent and few-shot context. |
| `timeout_seconds` | int | no | `30` | Per-request timeout for the chat endpoint. |

Reasoning is turned off on every request (`chat_template_kwargs.enable_thinking`, `think`, `reasoning_effort`, whichever your server understands) — a thinking model otherwise spends the whole token budget on its scratchpad and returns an empty answer. Endpoints that reject those parameters are detected on the first call and skipped from then on.

### How it works (and how it stays out of the way of rules)

The categorizer is built to **complement** Firefly III's rule engine, never to fight it:

1. **Leaves existing values alone by default.** If a transaction already carries a category (for example one parsed from the source CSV via a [`transactions`](#transactions) category column), it is left untouched — unless `overwrite_existing` is set, in which case the noisy label is re-mapped onto the closest matching Firefly category/budget. Even then the value is only ever replaced with another existing one, never blanked out.
2. **Constrained to what exists (and is in use).** The model may only choose from categories and budgets that already exist in Firefly, so it never creates entries a rule doesn't know about. The candidate list is further narrowed to keep prompts small for local models: **active budgets** (regardless of transaction count) and **categories that have at least one transaction** — inactive budgets and empty categories are dropped.
3. **Reuse before asking.** For each transaction it searches Firefly for similar past transactions (matched on a keyword from the description). If a strict majority of them already share a category/budget — typically because a rule or you assigned it — that value is reused directly and the model is not consulted. The model is only asked when there is no clear precedent, and its answer is discarded unless it exactly matches an existing name. Set `always_ask_model: true` to skip this shortcut and let the model decide every time (the historical values are still handed to it as few-shot context).

Enrichment is **best-effort**: any failure (endpoint down, timeout, unparseable reply) is logged as a warning and the transaction is uploaded without AI-assigned fields.

> [!TIP]
> On tiny local models (e.g. `qwen` on a Jetson Nano) context is scarce. Keep your Firefly category/budget lists modest and `max_examples` low — the prompt embeds the full allowed lists plus the examples, so both directly affect the token count.

---

## `browser_exec_path`

```yaml
browser_exec_path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

Absolute path to the Chromium-based browser executable used for web automation. The file must exist on disk (validated at startup).

Common paths:

| Platform | Brave Path |
|---|---|
| macOS | `/Applications/Brave Browser.app/Contents/MacOS/Brave Browser` |
| Linux | `/usr/bin/brave-browser` |

---

## `institutions`

A list of financial institutions. Each institution defines a login flow and one or more accounts to sync.

```yaml
institutions:
  - name: "Example"
    login:
      - type: navigate
        url: "https://bank.example.com/"
      # ... more steps
    # logout: [...]  # optional; sign out after the accounts are synced
    accounts:
      - name: "Checking"
        # ...
```

### Institution Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable institution name. Used in logs and with the `-institution` flag. |
| `login` | array of [steps](#browser-step-reference) | yes (min 1) | Steps executed once to authenticate with the institution. The browser session persists for all accounts under this institution. |
| `logout` | array of [steps](#browser-step-reference) | no | Optional steps executed once, after all of the institution's accounts have been synced, to sign out of the site and tear the session down. Runs even when some accounts failed (a live session is exactly what must not linger); skipped entirely when login failed. A failed logout is logged as a warning and never fails the run. Omit it to keep the session alive in the persistent browser profile between runs. |
| `accounts` | array | yes (min 1) | List of accounts to sync within this institution. |

---

### `accounts`

Each account maps a bank/brokerage account to a Firefly III account and defines how to scrape its data.

```yaml
accounts:
  - name: "Credit Card"
    firefly_account_id: 1
    account_type: "regular"
    balance: [...]
    transactions: [...]
```

#### Account Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable account name. Used in logs. |
| `firefly_account_id` | integer | yes | ID of the corresponding account in Firefly III. Find it in **Firefly → Accounts → (account) → URL**. |
| `account_type` | string | yes | Either `"regular"` or `"investment"`. Determines which flows are required. |
| `balance` | array of steps | conditional | Steps to scrape the current balance. Required for `regular` accounts. Must end with a [`balance`](#balance) step. |
| `transactions` | array of steps | conditional | Steps to download and parse a transaction file. Required for `regular` accounts. Must include at least one [`transactions`](#transactions) step. |
| `holdings` | array of steps | conditional | Steps to scrape stock holdings. Required for `investment` accounts. Must include at least one [`holdings`](#holdings) step. |

#### Account Type: `regular`

Use for checking, savings, and credit card accounts.

- **`balance`** — Required. A sequence of browser steps that navigates to the balance and ends with a `balance` step. The last step **must** be of type `balance`.
- **`transactions`** — Required. A sequence of browser steps that triggers a CSV/Excel file download and ends with (or includes) a `transactions` step. Must contain at least one `transactions` step.

The sync logic for regular accounts:
1. Scrape the current balance.
2. Compare it to the balance already in Firefly.
3. If the balance is unchanged and the last sync was recent (within `-sync-days`), skip CSV download.
4. Otherwise, download the transaction file, deduplicate against existing Firefly transactions, and upload new ones.

#### Account Type: `investment`

Use for brokerage/investment accounts that hold securities.

- **`holdings`** — Required. A sequence of browser steps that evaluates JavaScript to retrieve current share quantities. Must contain at least one `holdings` step.

The sync logic for investment accounts:
1. Evaluate the holdings JavaScript to get `{symbol: quantity}` pairs.
2. Compare to the holdings currently stored in the Firefly account's notes field.
3. Update Firefly only if the holdings have changed.

Holdings are stored in the Firefly account notes field as a comma-separated string: `AAPL=100.5,MSFT=50.25`. The key format is `<market-prefix>:<symbol>`, where the prefix tells the bridge which data source to use when fetching the current price for portfolio valuation.

**Market prefixes:**

| Prefix | Symbol format | Data source |
|---|---|---|
| _(none / any other value)_ | Stock ticker, e.g. `AAPL` | Yahoo Finance — `https://query2.finance.yahoo.com/v8/finance/chart/<symbol>` |
| `mi` | Fund path on Markets Insider, e.g. `mi:vanguard-target-retirement-2045-fund-us92202e6077` | Markets Insider — `https://markets.businessinsider.com/funds/<symbol>` |
| `mc` | Fund path on MoneyControl, e.g. `mc:MUT029` | MoneyControl — `https://www.moneycontrol.com/mutual-funds/nav/find/MUT029` |
| `gold` | Purity in parts-per-thousand, e.g. `gold:916` (22k) or `gold:999` (24k) | Kitco — `https://api.kitco.com/api/v1/precious-metals/au/` — spot bid price × (purity ÷ 1000) |
| `cash` | Any value (ignored), e.g. `cash:USD` | Always returns `1` — use for cash, money-market positions, or private symbols with no public price feed (e.g. 401k trust funds) |

---

## `vendors`

Optional. All-in-one merchants produce charges whose category can't be inferred from the bank description alone — the same store might be pet supplies one week, medicine the next, household goods after that. A vendor entry teaches the bridge to log into the merchant's own website (using the same [browser steps](#browser-step-reference) as institutions), pull its recent order history, and match each new bank charge to an order by **amount + date**. A matched charge is then categorized from the order's actual contents; the item list is also written into the transaction's notes for later review.

Vendor scraping is **on-demand only** — it never runs during a normal sync. Enable it per run with `-vendors all` or `-vendors "Name,Other"`. It also requires [`ai`](#ai) to be enabled, since the order contents are categorized by the model. To verify a vendor's login and orders flows without syncing anything, run `-list-orders` — it logs in, prints every scraped order, and exits.

```yaml
vendors:
  - name: "Example Store"
    match: "EXAMPLE ?STORE|EXMPLSTR" # regex routing bank descriptions to this vendor
    date_window_days: 3              # charge may post up to N days from the order date
    date_format: "2006-01-02"        # Go layout of dates returned by the orders JS
    login:
      - type: navigate
        url: "https://store.example.com/account/orders"
      # ... send_keys / click steps, same DSL as institution logins
    # logout:                        # optional; sign out after orders scraped
    #   - type: click
    #     selector: "#account-menu"
    #   - type: click
    #     selector: "#sign-out"
    orders:
      - type: navigate
        url: "https://store.example.com/account/orders"
      - type: orders
        evaluate: |
          (() => [...document.querySelectorAll('.order-row')].map(row => ({
            date: "...", amount: "...", items: "...",
          })))()
```

### Vendor Fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes | — | Vendor name. Used in logs, the review report, and with the `-vendors` flag. |
| `match` | string | yes | — | Regular expression, applied **case-insensitively** against each bank transaction's description, that routes the transaction to this vendor. Cover every form your statement uses. Validated at config load. |
| `date_window_days` | integer | no | `3` | How many days a charge date may differ from the order date and still match — cards typically charge at shipment, a few days after the order. An explicit `0` means same-day only. |
| `date_format` | string | no | _common layouts_ | Go `time.Parse` layout for the `date` strings returned by the orders JS. When omitted, common layouts are tried: `"2006-01-02"`, RFC3339, `"January 2, 2006"`, `"Jan 2, 2006"`, `"01/02/2006"`. |
| `login` | array of [steps](#browser-step-reference) | yes (min 1) | — | Steps executed once to authenticate with the vendor. Runs in the same persistent browser profile as institutions, so a manually-completed 2FA challenge survives across runs. |
| `logout` | array of [steps](#browser-step-reference) | no | — | Optional steps executed after the vendor's orders have been scraped (both `-vendors` and `-list-orders` runs) to sign out of the site. A failed logout is logged as a warning and never fails the run. |
| `orders` | array of [steps](#browser-step-reference) | yes (min 1) | — | Steps to scrape the order history. Must include at least one [`orders`](#orders) step. |

### When a vendor's login can't be automated

Two escalating situations, both worth recognising early:

**1. Session persists, login is scriptable.** The normal case. The browser profile lives in `chromedp-data/`, so start the login flow by navigating to an authenticated page and using [`skip_remaining_if`](#skip_remaining_if) to end the flow when you're already signed in. The credential steps then only run when the session has actually expired, and any 2FA challenge you complete by hand survives to later runs.

**2. Bot detection rejects the browser itself.** Some merchants front sign-in with a bot manager that fingerprints the *browser*, not just the automation. The tell is a sign-in POST that fails with **HTTP 429 or a challenge page even when you type the password by hand**, while the same credentials succeed in your normal browser on the same IP. No selector or timing change fixes this — stop tuning the flow.

For those, don't log in through the browser at all. Authenticate with a **long-lived token extracted once from your own browser session** and have the `orders` step call the merchant's API directly:

- Sign in normally in your own browser, open the order-history page, and copy the session/refresh token out of DevTools → **Application** → **Local Storage** (SPAs commonly cache one there).
- Store it in your secret manager and reference it from the `orders` JavaScript — secret references are resolved **anywhere inside the JS**, so the token never sits in your config.
- Have the JS exchange or present that token, then call the merchant's own order API. Make it `throw` on an auth failure so an expired token surfaces as a clear step error instead of an empty order list.

Tokens obtained this way expire (often 30–90 days); re-extract when the step starts failing.

> Merchant order APIs found this way are typically undocumented and internal. You're reading your own purchase history, but such endpoints carry no stability or terms-of-service guarantee and may change without notice.

### How vendor enrichment works

1. With `-vendors`, each requested vendor is logged into and its orders scraped **before** any institution runs. A vendor that fails to log in or scrape is skipped with a warning — its charges simply fall back to normal AI enrichment.
2. For each new transaction, the description is tested against every vendor's `match` regex. Non-matching transactions are enriched exactly as before.
3. A matching transaction is compared against that vendor's orders: same absolute amount (to the cent) and a date within `date_window_days`.
   - **Exactly one order matches** → the model assigns category/budget from the order's item list instead of the bank description, and the items are stored in the transaction's notes. If the order carries a merchant-provided `category` that already exists in Firefly, it is used directly without a model call. When the order supplies `line_items` spanning several categories, the charge is uploaded as a [split transaction](#splitting-an-order-across-categories) instead.
   - **No order, or several different orders, match** → the bridge **abstains**: the transaction is uploaded without a category/budget (so a Firefly rule or you can assign it), and the charge is added to the review report.
4. Every abstained charge is logged as it happens, with the vendor and the reason, e.g. `abstaining on "MEGASTORE.COM" (Example Store): no order of 45.67 within 3 day(s)`.

---

## Browser Step Reference

Steps are YAML objects with a required `type` field. They are used inside `login`, `balance`, `transactions`, `holdings`, and `orders` flows.

---

### `navigate`

Navigates the browser to a URL.

```yaml
- type: navigate
  url: "https://bank.example.com/"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | URL to navigate to. Must be a valid HTTP or HTTPS URL. |

---

### `wait_visible`

Pauses execution until a DOM element becomes visible. Use this to wait for pages or components to finish loading before interacting with them.

```yaml
- type: wait_visible
  selector: "#login-form"

# Or, using a JavaScript path:
- type: wait_visible
  js_path: "document.querySelector('#login-form')"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `js_path` | CSS selector for the element to wait for. |
| `js_path` | string | one of `selector` or `js_path` | JavaScript expression that evaluates to a DOM element. |

---

### `wait_not_visible`

Pauses execution until a DOM element is no longer visible. Useful for waiting for loading spinners or overlays to disappear.

```yaml
- type: wait_not_visible
  selector: "#loading-spinner"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `js_path` | CSS selector for the element to wait to disappear. |
| `js_path` | string | one of `selector` or `js_path` | JavaScript expression that evaluates to a DOM element. |

---

### `wait_not_present`

Pauses execution until a DOM element is completely removed from the DOM. Unlike [`wait_not_visible`](#wait_not_visible), this waits for the element to no longer exist in the document at all — not merely be hidden. Useful when an element is fully destroyed after a transition rather than just hidden.

```yaml
- type: wait_not_present
  selector: "#modal-dialog"

# Or, using a JavaScript path:
- type: wait_not_present
  js_path: "document.querySelector('#modal-dialog')"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `js_path` | CSS selector for the element to wait to be removed from the DOM. |
| `js_path` | string | one of `selector` or `js_path` | JavaScript expression that evaluates to a DOM element. |

---

### `click`

Clicks a DOM element.

```yaml
- type: click
  selector: "#signin-button"

# Or with a JS path:
- type: click
  js_path: "document.querySelector('#signin-button')"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `js_path` | CSS selector of the element to click. |
| `js_path` | string | one of `selector` or `js_path` | JavaScript expression that evaluates to a DOM element. |

---

### `sleep`

Pauses execution for a fixed duration. Use sparingly — prefer [`wait_visible`](#wait_visible) or [`wait_not_visible`](#wait_not_visible) for reliability. Sleeps are sometimes necessary to let JavaScript animations or async operations settle.

```yaml
- type: sleep
  duration: "2s"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `duration` | duration string | yes | How long to pause. Uses Go duration syntax: `"500ms"`, `"2s"`, `"1m30s"`. |

---

### `reload`

Reloads the current page (equivalent to pressing F5).

```yaml
- type: reload
```

No additional fields.

---

### `send_keys`

Types text into a form field using keyboard input events. This simulates a user typing, which can trigger `oninput` / `onchange` JavaScript handlers that `set_value` may not.

```yaml
- type: send_keys
  selector: "#username-field"
  value: "myusername"

# With a secret reference:
- type: send_keys
  selector: "#password-field"
  value: "op://vault/item/field"

# With a template:
- type: send_keys
  selector: "#start-date"
  value: '{{ SubtractDays 30 "01/02/2006" }}'
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `js_path` | CSS selector of the input element. |
| `js_path` | string | one of `selector` or `js_path` | JavaScript expression that evaluates to a DOM element. |
| `value` | string | yes | Text to type. Supports [secret references](#secret-references-in-values) and [template functions](#template-functions). |

---

### `set_value`

Sets the value of a form field directly via JavaScript's `.value` property. Faster than `send_keys` but may not trigger all JavaScript event handlers.

```yaml
- type: set_value
  selector: "#date-picker"
  value: '{{ Today "2006-01-02" }}'

# With a secret reference:
- type: set_value
  selector: "#api-key-field"
  value: "op://vault/item/field"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `js_path` | CSS selector of the form element. |
| `js_path` | string | one of `selector` or `js_path` | JavaScript expression that evaluates to a DOM element. |
| `value` | string | yes | Value to set. Supports [secret references](#secret-references-in-values) and [template functions](#template-functions). |

---

### `balance`

Extracts the balance text from the page and stores it for the account sync. **Must be the final step in a `balance` flow.** The extracted string is parsed into a number using the [amount parser](#amount-parsing).

```yaml
# Extract text content from a CSS selector:
- type: balance
  selector: "#account-balance-value"

# Or evaluate arbitrary JavaScript:
- type: balance
  evaluate: "document.querySelector('.balance').innerText"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `selector` | string | one of `selector` or `evaluate` | CSS selector. The visible text content of the element is used as the balance string. |
| `evaluate` | string | one of `selector` or `evaluate` | JavaScript expression. The return value (coerced to a string) is used as the balance string. |

**CSS selector escaping:** CSS selectors with IDs that start with digits must be escaped. For example, element `id="1234-balance"` becomes selector `#\31 234-balance` in CSS (and `#\\31 234-balance` in YAML strings).

---

### `transactions`

Waits for a file to be downloaded (triggered by previous steps, e.g., clicking a "Download CSV" button), then parses it into transactions. Must appear in a `transactions` flow.

```yaml
- type: transactions
  csv:
    options: { ... }
    fields: { ... }

# Or for Excel files:
- type: transactions
  excel:
    worksheet: 1
    options: { ... }
    fields: { ... }
```

Exactly one of `csv` or `excel` must be specified.

Multiple `transactions` steps in a single flow are supported — their results are merged.

---

#### CSV Mode

```yaml
- type: transactions
  csv:
    options:
      delimiter: ","
      skip_head_rows: 1
      skip_tail_rows: 0
      skip_row_conditions:
        - column: 1
          operation: "equals"
          value: "TOTAL"
    fields:
      date:
        column: 1
        format: "01/02/2006"
      description:
        column: 2
      amount:
        column: 3
```

#### Excel Mode

```yaml
- type: transactions
  excel:
    worksheet: 1
    password: "op://Vault/Item/excel-password"  # optional; plain string or op:// ref
    options:
      skip_head_rows: 1
    fields:
      date:
        column: 1
        format: "2006-01-02"
      description:
        column: 2
      amount:
        column: 3
```

| Field | Type | Required | Description |
|---|---|---|---|
| `worksheet` | integer | yes | 1-based index of the worksheet to parse (1 = first sheet, 2 = second, etc.). |
| `password` | string | no | Password to unlock a password-protected Excel file. Supports inline `op://` and `bw://` secret references. |
| `options` | object | no | Parsing options. See [`options`](#options). |
| `fields` | object | yes | Column mapping. See [`fields`](#fields). |

---

#### `options`

Controls how the raw file is pre-processed before field extraction.

```yaml
options:
  delimiter: ","
  skip_head_rows: 1
  skip_tail_rows: 2
  skip_row_conditions:
    - column: 1
      operation: "empty"
```

| Field | Type | Default | Description |
|---|---|---|---|
| `delimiter` | string | `","` | Single character used as the column separator. For tab-separated files use `"\t"`. Only the first character of the string is used. |
| `skip_head_rows` | integer | `0` | Number of rows to remove from the **beginning** of the file before processing (e.g., skip header rows). |
| `skip_tail_rows` | integer | `0` | Number of rows to remove from the **end** of the file before processing (e.g., skip totals rows). |
| `skip_row_conditions` | array | `[]` | Rows matching **any** condition are skipped entirely. See [MatchCondition](#skip_row_conditions--negate_if). |

---

#### `fields`

Maps transaction attributes to CSV column numbers. All column indices are **1-based** (column 1 = first column).

```yaml
fields:
  date:
    column: 1
    format: "01/02/2006"
  description:
    column: 2
  category:
    column: 3
  amount:
    column: 5
    negate: false
    negate_if:
      - column: 6
        operation: "contains"
        value: "DEBIT"
```

| Field | Required | Description |
|---|---|---|
| `date` | yes | Transaction date. |
| `date.column` | yes | Column number containing the date string. |
| `date.format` | yes | Go `time.Parse` layout for parsing the date (see [Go time formats](https://pkg.go.dev/time#Layout)). Common formats: `"01/02/2006"` (MM/DD/YYYY), `"2006-01-02"` (ISO 8601), `"1/2/06"` (M/D/YY). |
| `description` | yes | Transaction description/merchant name. |
| `description.column` | yes | Column number. |
| `category` | no | Transaction category. Mapped to the Firefly category name. |
| `category.column` | no | Column number. Omit or set to `0` to leave category blank. |

---

#### Amount Columns

Amount can be configured in one of two mutually exclusive ways:

**Option A — Single `amount` column** (contains both positive and negative values):

```yaml
fields:
  amount:
    column: 5
    negate: false
    negate_if:
      - column: 6
        operation: "contains"
        value: "CR"
```

| Field | Type | Default | Description |
|---|---|---|---|
| `amount.column` | integer | — | Column number of the amount. |
| `amount.negate` | boolean | `false` | If `true`, always multiply the parsed amount by `-1`. |
| `amount.negate_if` | array | `[]` | Conditionally negate the amount if the row matches any condition. Applied after `negate`. See [MatchCondition](#skip_row_conditions--negate_if). |

**Option B — Separate `debit` and `credit` columns** (one is blank per row):

```yaml
fields:
  debit:
    column: 4
    negate: false
  credit:
    column: 5
    negate: false
```

`debit` and `credit` must always be specified together. The parser checks which column is non-empty for each row.

| Field | Type | Default | Description |
|---|---|---|---|
| `debit.column` | integer | **required** | Column number of the debit (money out) amount. |
| `debit.negate` | boolean | `false` | By default debits are treated as negative (money leaving). Set to `true` to flip this behavior. |
| `debit.negate_if` | array | `[]` | Conditionally negate the debit amount. Applied after `negate`. See [MatchCondition](#skip_row_conditions--negate_if). |
| `credit.column` | integer | **required** | Column number of the credit (money in) amount. |
| `credit.negate` | boolean | `false` | By default credits are kept positive (money arriving). Set to `true` to negate them. |
| `credit.negate_if` | array | `[]` | Conditionally negate the credit amount. Applied after `negate`. See [MatchCondition](#skip_row_conditions--negate_if). |

**Amount sign convention:**
- Negative amounts → Firefly transaction type `withdrawal` (money out of the account)
- Positive amounts → Firefly transaction type `deposit` (money into the account)

---

#### `skip_row_conditions` / `negate_if`

`MatchCondition` objects are used in both `options.skip_row_conditions` and `fields.amount.negate_if` / `fields.debit.negate_if` / `fields.credit.negate_if`. A match is when **any** condition in the array is satisfied.

```yaml
- column: 1
  operation: "equals"
  value: "TOTAL"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `column` | integer | yes | 1-based column index to inspect. |
| `operation` | string | yes | Comparison operation (see below). |
| `value` | string | conditional | The string to compare against. Required for `equals`, `contains`, `starts_with`, `ends_with`. Not used for `empty` or `not_empty`. |

**Available operations:**

| Operation | Description | `value` required |
|---|---|---|
| `equals` | Cell matches `value` exactly (case-sensitive) | yes |
| `contains` | Cell contains `value` as a substring | yes |
| `starts_with` | Cell begins with `value` | yes |
| `ends_with` | Cell ends with `value` | yes |
| `empty` | Cell is empty or contains only whitespace | no |
| `not_empty` | Cell contains at least one non-whitespace character | no |

---

### `holdings`

Evaluates a JavaScript expression in the browser that returns an object mapping ticker symbols to share quantities. Used in `investment` account `holdings` flows.

```yaml
- type: holdings
  evaluate: |
    (() => {
      let result = {};
      document.querySelectorAll('[data-symbol]').forEach(el => {
        result[el.dataset.symbol] = parseFloat(el.dataset.quantity);
      });
      return result;
    })()
```

| Field | Type | Required | Description |
|---|---|---|---|
| `evaluate` | string | yes | JavaScript code to evaluate. Must return a plain object where keys are ticker symbols and values are numeric quantities. |

**Return format:** The JavaScript must evaluate to an object like:
```js
{
  "AAPL": 100.5,          // plain ticker → Yahoo Finance
  "mi:IE00B3RBWM25": 120, // Markets Insider fund
  "mc:MF_XXXXX": 45.67,   // MoneyControl mutual fund
  "gold:916": 10.5,        // 22k gold, quantity in grams
  "cash:USD": 5000         // cash position, always priced at 1
}
```

- Keys use the format `<market-prefix>:<symbol>` (see the market prefix table in the [Account Type: investment](#account-type-investment) section). A bare ticker with no prefix is treated as a Yahoo Finance stock symbol.
- Values must be numeric (`number` type). Integer, float64, or int64 are all accepted.

Multiple `holdings` steps in a single flow are supported — their results are **merged** into a single holdings map. This is useful when holdings are spread across multiple sections of a page.

---

### `orders`

Retrieves a [vendor's](#vendors) order history by evaluating JavaScript that returns an **array of order objects**. Used only inside a vendor's `orders` flow, which must contain at least one such step. Async functions are awaited, and `op://` / `bw://` secret references embedded in the JavaScript are resolved before evaluation — so an API token can be used without ever appearing in your config.

```yaml
- type: orders
  evaluate: |
    (() => [...document.querySelectorAll('.order-row')].map(row => ({
      date: "2026-07-29",
      amount: "199.95",
      items: "Dog food, vitamins",
      line_items: [
        { name: "Dog food", amount: "142.37" },
        { name: "Vitamins", amount: "38.91" },
      ],
    })))()
```

Each object supports these keys:

| Key | Required | Description |
|---|---|---|
| `date` | yes | Order date, parsed with the vendor's `date_format` (or common layouts when unset). |
| `amount` | yes | Order total. Currency symbols, thousands separators and a leading `-` are tolerated; the value is compared on its absolute value. |
| `items` | no | Human-readable item summary. Used as the description the model categorizes on, and written into the transaction's notes. Derived from `line_items` names when omitted. |
| `category` | no | Merchant-provided category. If it matches an existing Firefly category it is applied directly, skipping a model call. |
| `line_items` | no | Array of `{ name, amount }`. Enables **split transactions** — see below. |

Multiple `orders` steps in one flow **accumulate**, so you can page through order history. Rows whose date or amount can't be parsed are skipped with a warning rather than failing the scrape. Run [`-list-orders`](README.md) to print exactly what your JavaScript returned; with `-debug` the raw JSON payload is logged too. Returning anything other than an array of these objects fails the step with the offending payload in the error.

#### Splitting an order across categories

One order often spans several categories — pet supplies and medicine in the same basket. Supplying `line_items` lets the bridge upload that charge as a Firefly **split transaction** instead of forcing one category onto the whole thing:

1. The model is asked to categorize **each line** in a single call (lines are numbered, so it echoes indices rather than long names — this keeps the response small for local models).
2. Lines are grouped by category. If they all land in the same one, it's not a split — the transaction is simply labelled with that category.
3. Otherwise each group becomes a split. Group amounts are scaled **proportionally** to the amount actually billed, so tax, fees and discounts are distributed fairly and the splits always sum to the charge exactly.
4. Every split inherits the original charge's date, accounts, tags, budget and `internal_reference`, so deduplication still recognises the group on later runs. The group's title is the original bank description; each split is described by its own items.

Lines the model can't categorize aren't dropped — their value is absorbed proportionally into the categorized splits. Splitting is skipped (and the charge categorized as a whole) when fewer than two lines carry a usable amount, or when a share would round to zero. Turn it off entirely with [`ai.split_orders: false`](#ai).

| Field | Type | Required | Description |
|---|---|---|---|
| `evaluate` | string | yes | JavaScript returning an array of order objects. Async functions are awaited. `op://` / `bw://` secret references embedded in the string are resolved before evaluation. |

---

### `evaluate`

Evaluates arbitrary JavaScript on the current page and discards the return value. Use this for side-effectful JS — e.g. making an API call and triggering a programmatic file download — where the result is handled inside the script itself rather than returned to the bridge.

Async functions are supported; the step awaits any returned Promise before continuing.

`op://` secret references can be embedded directly inside the JavaScript string and are resolved before evaluation.

```yaml
- type: evaluate
  evaluate: |
    (async () => {
      const token = await fetch('https://api.example.com/token', {
        method: 'POST',
        body: JSON.stringify({
          username: 'op://Vault/Item/username',
          password: 'op://Vault/Item/password',
        }),
      }).then(r => r.json()).then(d => d.access_token);

      const data = await fetch('https://api.example.com/balance', {
        headers: { Authorization: `Bearer ${token}` },
      }).then(r => r.json());

      const blob = new Blob([JSON.stringify(data)], { type: 'application/json' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = 'result.json';
      a.click();
    })()
```

| Field | Type | Required | Description |
|---|---|---|---|
| `evaluate` | string | yes | JavaScript to evaluate. Async functions are awaited. The return value is ignored. `op://` secret references embedded in the string are resolved before evaluation. |

---

### `skip_remaining_if`

Evaluates a JavaScript expression and, when the result is truthy, ends the current flow early — successfully, skipping every remaining step.

Its main use is at the top of login flows: the browser profile persists between runs (`chromedp-data/`), so a previous session is often still valid and the credential steps would otherwise hang waiting for a login form that never appears. Land on an authenticated page first, then skip the rest of the flow unless you were bounced to the sign-in page.

```yaml
- type: navigate
  url: "https://store.example.com/account/orders"
- type: sleep
  duration: "4s"
# Still signed in → not redirected to the login page → skip the credential steps.
- type: skip_remaining_if
  evaluate: "!/login|signin/i.test(location.href)"
- type: send_keys
  selector: "#email"
  value: "op://my-vault/example-store/username"
# ...
```

Truthiness follows JavaScript rules: `false`, `0`, `""`, `null`/`undefined` continue the flow; everything else skips.

Steps that produce results (`balance`, `transactions`, `holdings`, `orders`) still have to run for their flow to succeed, so place `skip_remaining_if` only in flows where skipping the remainder is genuinely fine — login flows, typically.

| Field | Type | Required | Description |
|---|---|---|---|
| `evaluate` | string | yes | JavaScript expression to evaluate. Async functions are awaited. A truthy result skips all remaining steps in the flow; a falsy one continues normally. `op://` secret references embedded in the string are resolved before evaluation. |

---

## Template Functions

The `value` field in [`send_keys`](#send_keys) and [`set_value`](#set_value) steps supports Go text/template syntax. Templates are processed after secret resolution. Use `{{ }}` delimiters.

### `Today`

Returns the current date formatted as a string.

```
{{ Today "format" }}
```

| Argument | Type | Description |
|---|---|---|
| `format` | string | Go time layout string |

**Examples:**
```yaml
value: '{{ Today "2006-01-02" }}'        # → "2026-02-21"
value: '{{ Today "01/02/2006" }}'        # → "02/21/2026"
value: '{{ Today "January 2, 2006" }}'  # → "February 21, 2026"
```

### `SubtractDays`

Returns the date N days in the past, formatted as a string.

```
{{ SubtractDays days "format" }}
```

| Argument | Type | Description |
|---|---|---|
| `days` | integer | Number of days to subtract from today |
| `format` | string | Go time layout string |

**Examples:**
```yaml
value: '{{ SubtractDays 30 "2006-01-02" }}'   # → "2026-01-22" (30 days ago)
value: '{{ SubtractDays 90 "01/02/2006" }}'   # → "11/23/2025" (90 days ago)
```

### Go Time Format Reference

Go uses a reference time of `Mon Jan 2 15:04:05 MST 2006` for format strings. Key components:

| Component | Meaning |
|---|---|
| `2006` | 4-digit year |
| `06` | 2-digit year |
| `01` | 2-digit month |
| `1` | Month without leading zero |
| `Jan` | Month abbreviation |
| `January` | Full month name |
| `02` | 2-digit day |
| `2` | Day without leading zero |
| `15` | 24-hour hour |
| `3` | 12-hour hour |
| `04` | Minutes |
| `05` | Seconds |

---

## Amount Parsing

All monetary amounts extracted from the page (via `balance` selector/evaluate) or from CSV/Excel cells are parsed with the same logic:

1. Find the first occurrence of a digit sequence with optional commas and an optional decimal point (regex: `[\d,]+\.?\d*`).
2. Remove all commas.
3. Parse as a 64-bit float.

**Examples:**

| Raw string | Parsed value |
|---|---|
| `"$1,234.56"` | `1234.56` |
| `"(123.45)"` | `123.45` |
| `"1,000"` | `1000` |
| `"USD 500.00"` | `500.00` |
| `"-42.50"` | `42.50` |
| `""` (empty) | `0` |

> **Note:** The parser extracts only the first digit sequence. Parentheses (common for negative values in bank statements) do not make the result negative — use `negate` or `negate_if` in your field config to handle sign.

---

## Validation Rules Summary

The config is fully validated on load. Errors are reported with field paths.

| Field | Rule |
|---|---|
| `firefly.host` | Required, valid HTTP/HTTPS URL |
| `firefly.token` | Required, valid JWT format |
| `browser_exec_path` | Required, file must exist on disk |
| `institutions` | Required, minimum 1 entry |
| `institution.name` | Required, non-empty |
| `institution.login` | Required, minimum 1 step |
| `institution.logout` / `vendor.logout` | Optional; standard per-step validation applies |
| `institution.accounts` | Required, minimum 1 entry |
| `account.name` | Required, non-empty |
| `account.firefly_account_id` | Required, positive integer |
| `account.account_type` | Required, must be `"regular"` or `"investment"` |
| `account.balance` _(regular)_ | Required; last step must be type `balance` |
| `account.transactions` _(regular)_ | Required; must contain at least one `transactions` step |
| `account.holdings` _(investment)_ | Required; must contain at least one `holdings` step |
| `navigate.url` | Required, valid HTTP/HTTPS URL |
| `wait_visible` | Requires `selector` or `js_path` (at least one) |
| `wait_not_visible` | Requires `selector` or `js_path` (at least one) |
| `click` | Requires `selector` or `js_path` (at least one) |
| `sleep.duration` | Required, valid Go duration string |
| `send_keys.value` | Required |
| `send_keys` | Requires `selector` or `js_path` (at least one) |
| `set_value.value` | Required |
| `set_value` | Requires `selector` or `js_path` (at least one) |
| `balance` | Requires `selector` or `evaluate` (at least one) |
| `transactions` | Requires exactly one of `csv` or `excel` |
| `transactions csv/excel fields` | Requires `date` and `description`; requires `amount` OR both `debit`+`credit`; cannot have both `amount` and `debit`/`credit` |
| `holdings.evaluate` | Required |
| `evaluate.evaluate` | Required |
| `match_condition.column` | Required, positive integer |
| `match_condition.operation` | Required, must be one of: `equals`, `contains`, `starts_with`, `ends_with`, `empty`, `not_empty` |
| `match_condition.value` | Required when `operation` is `equals`, `contains`, `starts_with`, or `ends_with` |
| `secrets.onepassword.token` | Required when onepassword block is present |
| `secrets.bitwarden` | All fields optional; requires the `bw` CLI installed and its vault unlocked |

---

## Full Example

See [`config.example.yaml`](config.example.yaml) for a complete, annotated example covering every step type, account type, field option, and market prefix.
