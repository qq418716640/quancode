# AgentConfig Schema

`quancode` uses a YAML config file to describe available coding agents and how each one should be launched or delegated to.

Config lookup order:

1. `--config <path>`
2. `./quancode.yaml`
3. `~/.config/quancode/quancode.yaml`
4. built-in defaults

## Top-Level Fields

### `default_primary`

- Type: string
- Required: yes
- Meaning: the agent key used by `quancode start` when `--primary` is not provided

### `agents`

- Type: map of agent key to `AgentConfig`
- Required: yes
- Meaning: declares all known agent adapters for the current installation

## AgentConfig Fields

### `name`

- Type: string
- Meaning: display name used in prompts and command output

### `command`

- Type: string
- Required: yes
- Meaning: executable name or absolute path used to launch the CLI

### `description`

- Type: string
- Meaning: short human-readable description shown in prompts and listings

### `strengths`

- Type: list of strings
- Meaning: capabilities shown to the primary agent when delegation options are injected

### `primary_args`

- Type: list of strings
- Default: empty
- Meaning: arguments passed when the agent is launched as the primary interactive CLI

### `delegate_args`

- Type: list of strings
- Default: empty
- Meaning: arguments passed when the agent is used for one-shot delegation

### `output_flag`

- Type: string
- Default: empty
- Meaning: flag used by CLIs that write their final answer to a file rather than stdout

### `timeout_secs`

- Type: integer
- Default: implementation-specific; non-positive values fall back internally
- Meaning: timeout for delegated commands

### `enabled`

- Type: boolean
- Meaning: whether the agent is available for selection and launch

### `env`

- Type: map of string to string
- Default: empty
- Meaning: environment overrides for that agent only

`quancode` merges these over the parent environment by key. Matching is case-insensitive.

### `preferred_for`

- Type: list of strings
- Meaning: routing keywords used by `quancode route` and automatic delegate selection

### `priority`

- Type: integer
- Meaning: lower numbers win when no routing keyword matches

## Adapter Fields

These fields control how the generic adapter interacts with each CLI.

### `prompt_mode`

- Type: string
- Supported values: `append_arg`, `env`, `stdin`, `file`
- Default: `append_arg`

Meaning:

- `append_arg`: append the generated prompt as the final CLI argument
- `env`: expose the prompt via `QUANCODE_SYSTEM_PROMPT`
- `stdin`: reserved for future expansion; not currently supported for primary interactive launch
- `file`: inject prompt content into a managed file such as `AGENTS.md`

### `prompt_file`

- Type: string
- Default: `AGENTS.md`
- Meaning: file name used when `prompt_mode=file`

### `task_mode`

- Type: string
- Supported values: `arg`, `stdin`
- Default: `arg`

Meaning:

- `arg`: delegated task is passed as the final CLI argument
- `stdin`: delegated task is piped to stdin

### `output_mode`

- Type: string
- Supported values: `stdout`, `file`
- Default: `stdout`

Meaning:

- `stdout`: read the delegated result from standard output
- `file`: expect the CLI to write its final output to a file referenced by `output_flag`

## Compatibility Notes

- Older config files may omit newer adapter fields. `quancode` backfills missing known-agent defaults at load time.
- Different CLIs may require different combinations of `prompt_mode`, `task_mode`, and `output_mode`.
- Avoid copying local proxy settings or machine-specific paths into shared example configs.

## Example

See [`quancode.example.yaml`](../quancode.example.yaml) for a starter config without machine-specific assumptions.
