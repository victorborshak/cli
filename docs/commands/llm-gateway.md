# `dr llm-gateway` — LLM model management

List available LLMs from LLM Gateway, DataRobot deployments, and an optionally configured LiteLLM proxy, then configure which one the CLI uses by default.

## Synopsis

```bash
dr llm-gateway <command> [flags]
dr llm [flags]           # alias
```

## Description

The `dr llm-gateway` group exposes two subcommands:

- **`list`** — fetch available LLMs from active LLM Gateway catalog models (`/api/v2/genai/llmgw/catalog/`), DataRobot-deployed LLMs (`/api/v2/deployments/`, deployments whose champion model is a `TextGeneration` model), and LiteLLM (`$DATAROBOT_LITELLM_BASE_URL/models`, when configured). A `SOURCE` column / `source` field distinguishes them. `--source` narrows it to one.
- **`select`** — choose a default LLM, either by ID or through an interactive TUI picker. The selection is persisted to `drconfig.yaml` and read by other CLI commands.

When all sources are queried (`select`, and `list` without `--source`), each is best-effort: if one cannot be reached (e.g. an empty LLM Gateway on-prem, no deployment access, or an unavailable LiteLLM proxy), the command logs a warning and lists the others, and errors only when every queried source fails. The sources are fetched in parallel, so the command waits on the slowest source. Asking `list` for a single source instead makes a failure to reach it an error, since there is no other source left to show.

**Aliases:** `llm`, `llm-gateways`

## Subcommands

### `list`

Fetch available LLMs from LLM Gateway catalog models, DataRobot-deployed LLMs, and an optionally configured LiteLLM proxy.

```bash
dr llm-gateway list [flags]
dr llm ls               # shortest alias
```

**Flags:**

- `--output-format <json>` — emit machine-parseable JSON instead of a table.
- `--source <all|gateway|deployed|litellm>` — which sources to query. Defaults to `all`. `gateway`, `deployed`, and `litellm` skip requests to the other sources, so a script that needs one source does not wait on them. `litellm` requires both `DATAROBOT_LITELLM_BASE_URL` and `DATAROBOT_LITELLM_API_KEY`. The values are the same strings the `SOURCE` column and the JSON `source` field carry. A single source that cannot be reached is an error rather than an empty list.

**Output columns (table):**

| Column     | Description                                      |
|------------|--------------------------------------------------|
| `ID`       | LLM identifier — a gateway model id, deployment id, or LiteLLM model id. Prefixed with `*` if selected, `  ` otherwise. |
| `NAME`     | Human-readable model name (a deployment's label for deployed LLMs; the model id for LiteLLM). |
| `SOURCE`   | `gateway` for LLM Gateway catalog models, `deployed` for DataRobot-deployed LLMs, or `litellm` for LiteLLM models. |
| `PROVIDER` | Provider (e.g. `azure`, `anthropic`, `google`); LiteLLM's `owned_by` when supplied. `-` for deployed LLMs. |
| `MODEL`    | Underlying model identifier. `-` for deployed LLMs (the deployment id in `ID` is the routing key). |
| `CONTEXT`  | Context-window size in tokens. `-` when not reported (always `-` for deployed LLMs). |

The table width is content-driven and capped at the terminal width to prevent overflow. `description` is omitted from the table (it is long enough to wrap unreadably across a full catalog) and appears in JSON output only.

**JSON output** (`--output-format json`) returns an envelope with a `llms` array. Each entry includes:

```json
{
  "id":            "llm-abc123",
  "name":          "GPT-4o",
  "source":        "gateway",
  "provider":      "azure",
  "model":         "gpt-4o",
  "description":   "OpenAI's flagship multimodal model.",
  "context_size":  128000,
  "deployment_id": "",
  "selected":      true
}
```

For a deployed LLM, `source` is `deployed`, `deployment_id` carries the deployment id, and `model` is the litellm sentinel `datarobot/datarobot-deployed-llm`.

**Examples:**

```bash
# Table view
dr llm-gateway list

# JSON output
dr llm-gateway list --output-format json

# One source only. Skips the request the other source would have made.
dr llm-gateway list --source gateway
dr llm-gateway list --source deployed --output-format json
dr llm-gateway list --source litellm

# Aliases
dr llm list
dr llm ls
```

---

### `select`

Set the default LLM. The chosen ID — a gateway model id or a deployment id — is written to `drconfig.yaml` under the key `default-llm-id` and is also readable via `DATAROBOT_CLI_DEFAULT_LLM_ID`.

```bash
dr llm-gateway select [llm-id]
dr llm select [llm-id]   # alias
```

**Arguments:**

- `[llm-id]` — optional. When provided, the ID is validated against the available LLMs (gateway models and deployed LLMs) and persisted immediately. When omitted, an interactive TUI picker is launched.

**Interactive picker:**

- Arrow keys to navigate, `/` to filter by name.
- `Enter` to confirm selection.
- `Ctrl-C` or `Esc` to cancel without saving.

**Examples:**

```bash
# Interactive TUI picker
dr llm-gateway select

# Set directly by ID
dr llm-gateway select llm-abc123

# Error — ID not found among available LLMs
dr llm-gateway select unknown-id
# Error: LLM "unknown-id" not found
```

---

## Configuration

The selected LLM ID is stored in `drconfig.yaml`. This is a gateway model id or a DataRobot deployment id, depending on which was selected:

```yaml
default-llm-id: llm-abc123
```

It can also be set or overridden via the environment variable:

```bash
export DATAROBOT_CLI_DEFAULT_LLM_ID=llm-abc123
```

To include models from LiteLLM in `list` and `select`, configure both:

```bash
export DATAROBOT_LITELLM_BASE_URL=https://your-litellm-proxy.example
export DATAROBOT_LITELLM_API_KEY=your-litellm-api-key
```

The `dr llm-gateway list` output uses this value to mark the currently selected model with `*`.

## Authentication

Both subcommands require valid DataRobot credentials. Run `dr auth login` first if you haven't already.

## See also

- [auth](auth.md) — authenticate with DataRobot.
- [Command reference](README.md) — overview of all commands.
