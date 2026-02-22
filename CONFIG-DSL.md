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
  - [Secret References in Values](#secret-references-in-values)
- [`browser_exec_path`](#browser_exec_path)
- [`institutions`](#institutions)
  - [Institution Fields](#institution-fields)
  - [`accounts`](#accounts)
    - [Account Fields](#account-fields)
    - [Account Type: `regular`](#account-type-regular)
    - [Account Type: `investment`](#account-type-investment)
- [Browser Step Reference](#browser-step-reference)
  - [`navigate`](#navigate)
  - [`wait_visible`](#wait_visible)
  - [`wait_not_visible`](#wait_not_visible)
  - [`click`](#click)
  - [`sleep`](#sleep)
  - [`reload`](#reload)
  - [`send_keys`](#send_keys)
  - [`set_value`](#set_value)
  - [`balance`](#balance)
  - [`transactions`](#transactions)
    - [CSV Mode](#csv-mode)
    - [Excel Mode](#excel-mode)
    - [`options`](#options)
    - [`fields`](#fields)
    - [Amount Columns](#amount-columns)
    - [`skip_row_conditions` / `negate_if`](#skip_row_conditions--negate_if)
  - [`holdings`](#holdings)
- [Template Functions](#template-functions)
- [Amount Parsing](#amount-parsing)
- [Validation Rules Summary](#validation-rules-summary)
- [Full Example](#full-example)

---

## Config File Basics

The main config file is YAML. By default, the bridge looks for `config.yaml` in the current directory; override with `-config <path>`.

### Environment Variable Expansion

Every value in the config file is run through `os.ExpandEnv` before parsing. You can use `$VAR` or `${VAR}` anywhere:

```yaml
firefly:
  host: "${FIREFLY_HOST}"
  token: "${FIREFLY_TOKEN}"
browser_exec_path: "${BROWSER_PATH}"
```

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

browser_exec_path: "/path/to/chrome"  # required

institutions:     # required, minimum 1
  - name: "..."
    login: [...]
    accounts: [...]
```

| Field | Type | Required | Description |
|---|---|---|---|
| `firefly` | object | yes | Firefly III API connection settings |
| `secrets` | object | no | Secret provider configuration |
| `browser_exec_path` | string | yes | Absolute path to the browser executable used for automation |
| `institutions` | array | yes (min 1) | List of financial institutions to sync |

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

Optional configuration for secret providers. When configured, `value` fields in [`send_keys`](#send_keys) and [`set_value`](#set_value) steps can reference secrets by URI instead of hardcoding credentials.

```yaml
secrets:
  onepassword:
    token: "ops_..."
```

### `secrets.onepassword`

Configures the 1Password secret provider using a Service Account token.

| Field | Type | Required | Description |
|---|---|---|---|
| `token` | string | yes | 1Password Service Account token (create one in **1Password → Developer → Service Accounts**) |

### Secret References in Values

Once a secret provider is configured, any `value` field in `send_keys` or `set_value` steps can use a secret URI:

```yaml
- type: send_keys
  selector: "#username"
  value: "op://vault-name/item-name/field-name"
```

**1Password URI format:** `op://vault/item/field`

- `vault` — name or UUID of the 1Password vault
- `item` — name or UUID of the item
- `field` — name of the field within the item (e.g., `username`, `password`)

If the value does not contain `://`, it is used as a literal string. Secret resolution happens before template parsing, so a value cannot be both a secret reference and a template expression.

---

## `browser_exec_path`

```yaml
browser_exec_path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

Absolute path to the Chromium-based browser executable used for web automation. The file must exist on disk (validated at startup).

Common paths:

| Platform | Chrome Path |
|---|---|
| macOS | `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome` |
| Linux | `/usr/bin/google-chrome` or `/usr/bin/chromium-browser` |
| Windows | `C:\Program Files\Google\Chrome\Application\chrome.exe` |

---

## `institutions`

A list of financial institutions. Each institution defines a login flow and one or more accounts to sync.

```yaml
institutions:
  - name: "First Bank"
    login:
      - type: navigate
        url: "https://bank.example.com/"
      # ... more steps
    accounts:
      - name: "Checking"
        # ...
```

### Institution Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable institution name. Used in logs and with the `-institution` flag. |
| `login` | array of [steps](#browser-step-reference) | yes (min 1) | Steps executed once to authenticate with the institution. The browser session persists for all accounts under this institution. |
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

## Browser Step Reference

Steps are YAML objects with a required `type` field. They are used inside `login`, `balance`, `transactions`, and `holdings` flows.

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
| `worksheet` | integer | yes (Excel only) | 1-based index of the worksheet to parse (1 = first sheet, 2 = second, etc.). |
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

### `Env`

Returns the value of an environment variable.

```
{{ Env "VAR_NAME" }}
```

| Argument | Type | Description |
|---|---|---|
| `VAR_NAME` | string | Name of the environment variable |

**Example:**
```yaml
value: '{{ Env "BANK_USERNAME" }}'
```

Returns an empty string if the variable is not set.

---

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
| `match_condition.column` | Required, positive integer |
| `match_condition.operation` | Required, must be one of: `equals`, `contains`, `starts_with`, `ends_with`, `empty`, `not_empty` |
| `match_condition.value` | Required when `operation` is `equals`, `contains`, `starts_with`, or `ends_with` |
| `secrets.onepassword.token` | Required when onepassword block is present |

---

## Full Example

```yaml
# config.yaml

firefly:
  host: "https://firefly.example.com"
  token: "${FIREFLY_TOKEN}"

secrets:
  onepassword:
    token: "ops_your_service_account_token"

browser_exec_path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

institutions:

  # ── Regular bank/credit card institution ───────────────────────────────────
  - name: "First Bank"
    login:
      - type: navigate
        url: "https://www.firstbank.example.com/"
      - type: wait_visible
        selector: "#login-form"
      - type: sleep
        duration: "2s"
      - type: send_keys
        selector: "#username-input"
        value: "op://vault/first-bank/username"
      - type: send_keys
        selector: "#password-input"
        value: "op://vault/first-bank/password"
      - type: click
        selector: "#signin-btn"
      - type: wait_not_visible
        selector: "#login-form"

    accounts:
      # Credit card — single amount column
      - name: "Credit Card"
        firefly_account_id: 1
        account_type: "regular"

        balance:
          - type: navigate
            url: "https://www.firstbank.example.com/dashboard"
          - type: wait_visible
            selector: "#card-accounts"
          - type: sleep
            duration: "2s"
          - type: balance
            selector: "#balance-value"

        transactions:
          - type: click
            selector: "#download-csv-btn"
          - type: sleep
            duration: "5s"
          - type: transactions
            csv:
              options:
                delimiter: ","
                skip_head_rows: 1
              fields:
                date:
                  column: 1
                  format: "01/02/2006"
                description:
                  column: 2
                category:
                  column: 3
                amount:
                  column: 4

      # Checking account — debit/credit split columns, skip empty rows
      - name: "Checking"
        firefly_account_id: 2
        account_type: "regular"

        balance:
          - type: navigate
            url: "https://www.firstbank.example.com/dashboard"
          - type: wait_visible
            selector: "#deposit-accounts"
          - type: balance
            evaluate: "document.querySelector('#balance').innerText"

        transactions:
          - type: navigate
            url: "https://www.firstbank.example.com/dashboard"
          - type: click
            selector: "#download-btn"
          - type: set_value
            selector: "#start-date"
            value: '{{ SubtractDays 30 "01/02/2006" }}'
          - type: set_value
            selector: "#end-date"
            value: '{{ Today "01/02/2006" }}'
          - type: click
            selector: "#export-btn"
          - type: sleep
            duration: "5s"
          - type: transactions
            csv:
              options:
                skip_head_rows: 1
                skip_row_conditions:
                  - column: 1
                    operation: "empty"
              fields:
                date:
                  column: 1
                  format: "01/02/2006"
                description:
                  column: 2
                debit:
                  column: 3
                credit:
                  column: 4

  # ── Investment/brokerage institution ───────────────────────────────────────
  - name: "Apex Brokerage"
    login:
      - type: navigate
        url: "https://www.apexbrokerage.example.com/"
      - type: wait_visible
        selector: "#username-input"
      - type: send_keys
        selector: "#username-input"
        value: "op://vault/apex-brokerage/username"
      - type: send_keys
        selector: "#password-input"
        value: "op://vault/apex-brokerage/password"
      - type: click
        selector: "#login-btn"
      - type: wait_not_visible
        selector: "#login-btn"

    accounts:
      - name: "Brokerage Account"
        firefly_account_id: 10
        account_type: "investment"

        holdings:
          - type: navigate
            url: "https://www.apexbrokerage.example.com/portfolio/positions"
          - type: wait_visible
            selector: "#positions-table"
          - type: sleep
            duration: "3s"
          - type: holdings
            evaluate: |
              (() => {
                const result = {};
                document.querySelectorAll('.position-row').forEach(row => {
                  const symbol = row.querySelector('.symbol').innerText.trim();
                  const qty = parseFloat(row.querySelector('.quantity').innerText.replace(/,/g, ''));
                  if (symbol && !isNaN(qty)) result[symbol] = qty;
                });
                return result;
              })()

  # ── Institution loaded from an external file ───────────────────────────────
  - !import "institutions/another-bank.yaml"
```
