# wallfacer

Command-line interface for the [Wallfacer API](https://api.wallfacer.ai). Built with [openapi-cli-generator](https://github.com/WallfacerTech/openapi-cli-generator), all commands are auto-generated from the OpenAPI spec.

## Installation

### From source

```bash
git clone https://github.com/WallfacerTech/wallfacer-cli.git
cd wallfacer-cli
make build
```

## Authentication

Store your API token (created via `POST /v1/tokens`):

```bash
wallfacer auth login --token=<your-token>
```

Check that it works:

```bash
wallfacer auth status
```

Remove a stored token:

```bash
wallfacer auth logout
```

## Usage

```bash
# List accounts
wallfacer accounts list

# Get a specific account
wallfacer accounts get <account-id>

# List environments for an account
wallfacer environments list <account-id>

# Create a task
wallfacer tasks create <account-id> --body '{"prompt": "fix the login bug"}'

# Get help for any command
wallfacer <command> --help
```

If you set `account_id` in the config file, the account-id argument is automatically injected and can be omitted.

## Configuration

Configuration is stored in `~/.wallfacer/wallfacer.yml`:

```yaml
token: <your-api-token>
account_id: <default-account-id>
base_url: https://api.wallfacer.ai  # optional override
```

Environment variables with the `WALLFACER_` prefix are also supported (e.g. `WALLFACER_TOKEN`).

## Development

The CLI commands in `openapi.go` are generated from `openapi.yaml`. To regenerate after updating the spec:

```bash
make generate
```

This requires the [openapi-cli-generator](https://github.com/WallfacerTech/openapi-cli-generator) repo to be cloned alongside this one (as `../openapi-cli-generator`).

Note: `go.mod` contains a `replace` directive that points to `../openapi-cli-generator` for local development. This means `go install` from a remote module path will not work -- you must build from a local clone with the sibling repo present.
