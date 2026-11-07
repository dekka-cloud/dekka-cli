# Dekka CLI

`dekka` deploys and runs applications on [Dekka](https://dekka.cloud) from your terminal and from CI. It calls the same public API as the dashboard, at `https://api.dekka.cloud/v1`, with an API key you create there, so every command passes the same permission checks the dashboard applies to you.

## Install

On macOS and Linux the install script downloads the latest release, checks it against the release's `checksums.txt`, and puts `dekka` in `/usr/local/bin`, or in `~/.local/bin` when that directory isn't writable.

```sh
curl -fsSL https://raw.githubusercontent.com/dekka-cloud/dekka-cli/main/install.sh | sh
```

Set `DEKKA_VERSION` to install a particular release, such as `v0.1.0`, and `DEKKA_INSTALL_DIR` to choose the directory. The [releases page](https://github.com/dekka-cloud/dekka-cli/releases) has archives for macOS, Linux and Windows on amd64 and arm64, each with an SBOM, and a checksums file signed with Sigstore.

No release has been tagged yet, so until the first one is, build the CLI with Go instead.

```sh
go install github.com/dekka-cloud/dekka-cli/cmd/dekka@latest
```

## Sign in

Create an API key in the dashboard at [dekka.cloud/app](https://dekka.cloud/app) under **Settings**, then **API keys**, and run `dekka login` to paste it. The CLI checks the key with the API and saves it in the system keychain, which is the Keychain on macOS, the Secret Service on Linux and the Credential Manager on Windows. On a machine with no keychain, such as a server without a desktop session, it saves the key in `dekka/credentials.json` under your user config directory with permissions that let only you read it, and tells you it has done so.

`dekka login` also reads the key from standard input, which keeps it out of your shell history, and `--token` passes it as an argument.

```sh
dekka login < dekka-key.txt
dekka whoami
```

`dekka whoami` shows who the key acts for and which organisation it belongs to. `dekka logout` removes the saved key from this machine, and the key stays valid until you revoke it in the dashboard.

In CI, set `DEKKA_TOKEN` to the key instead of running `dekka login`, because it takes precedence over a saved key. `DEKKA_API_URL` points the CLI at a different Dekka API, which is only useful when you work on Dekka itself.

## Commands

| Command | What it does |
| --- | --- |
| `dekka projects list` | Lists the organisation's projects and their environments |
| `dekka apps list [--project] [--environment]` | Lists applications, workers and cron jobs with their status and address |
| `dekka deploy <service> [--branch] [--commit]` | Starts a deployment of a service |
| `dekka status <service>` | Shows a service and its five most recent deployments |
| `dekka logs <service> [--deployment <id>] [--follow]` | Prints a service's runtime logs, or one deployment's build and deploy logs |
| `dekka rollback <deployment> [--keep-current-variables] [--yes]` | Deploys an earlier deployment again |

A service is named by its name, slug or id. The same name can appear in several projects and environments, so `--project` and `--environment` narrow the search, and a name that still matches more than one service is refused with each match listed.

Commands print a table or a short summary, and `--output json` (or `-o json`) prints JSON with the API's field names instead. With `--output json`, `dekka logs` prints one JSON object per line.

A typical release from a terminal or a CI job looks like this.

```sh
dekka deploy api --branch main
dekka logs api --deployment <deployment id> --follow
dekka status api
```

`dekka deploy` prints the new deployment's id and the command that follows it. Following a deployment's logs streams them until the deployment finishes, and the command exits with status 1 unless it ends healthy, so a CI job can deploy and wait on the result. Following runtime logs keeps printing new lines until you press Ctrl-C.

`dekka rollback` deploys the image that an earlier healthy deployment ran, with the settings and the variables it ran with; `--keep-current-variables` keeps the service's variables as they are now instead. Databases, volumes and buckets are not changed, so their data and any migrations stay as they are. Before it calls the API it names the service, its project and environment, and the deployment it returns to, and asks you to confirm. `--yes` skips the question, and without a terminal the command needs `--yes`.

## Keys and permissions

The CLI has no access of its own. It sends your key with every request, and the API decides what the key may do with the same checks it applies to the dashboard, so the CLI can never reach anything the key's creator couldn't reach in the browser.

An [API key](https://docs.dekka.cloud/api-keys) belongs to one organisation, and each scope caps it at a role. The `read` scope acts as a viewer and is enough for `whoami`, `projects list`, `apps list`, `status` and `logs`, while `write` acts as a developer and is needed for `deploy` and `rollback`. The `admin` scope manages organisation settings, which the CLI doesn't do yet, and a key with only the `ai` scope reaches the AI gateway and nothing the CLI uses. A key never does more than the person who created it, so a developer's key with `admin` still acts as a developer.

A [project token](https://docs.dekka.cloud/project-tokens) is a key that reaches one project with `read` or `write`, which makes it the safer choice for CI. It can't list the organisation's projects, so pass the project's id with `--project` when you name a service, or name the service by its id.

When a key can't see something the API answers 404, and when it can see it but not change it the API answers 403. The CLI prints the API's message with its error code and request id, which is what Dekka support needs if you ask for help.

## What comes next

`dekka plan` and `dekka apply` will preview and apply an environment described in a `dekka.toml` file once the API supports environment files, and neither command exists yet.

## Development

The API client in `internal/api` is generated with oapi-codegen from `openapi/openapi.json`, a copy of the document the public API serves at `https://api.dekka.cloud/openapi.json`. `make sync-spec` fetches it again and regenerates the client, and CI fails when regenerating from the copy changes anything. A scheduled workflow also fails when the published document no longer matches the copy.

CI runs the tests, golangci-lint and those checks on GitHub-hosted `ubuntu-latest` runners, which are free for public repositories, and a tag starting with `v` builds a release with GoReleaser on the same runners. [CONTRIBUTING.md](CONTRIBUTING.md) explains how to build and test the CLI, and [SECURITY.md](SECURITY.md) explains how to report a vulnerability.

## License

Apache License 2.0, in [LICENSE](LICENSE).
