# Dekka CLI

`dekka` deploys and runs applications on [Dekka](https://dekka.cloud) from your terminal and from CI. It calls the same public API as the dashboard, at `https://api.dekka.cloud/v1`, with an API key you create there, so every command passes the same permission checks the dashboard applies to you.

## Install

With Node.js installed, `npx` runs the CLI with nothing to install first, so one command signs a new machine in.

```sh
npx dekka@latest login
```

The package downloads the release for your machine on first use, checks it against the release's checksums, and keeps it for the next run. `npm install -g dekka` puts `dekka` on your `PATH`, which is how the rest of this page writes the commands.

Without Node.js, on macOS and Linux the install script downloads the latest release, checks it against the release's `checksums.txt`, and puts `dekka` in `/usr/local/bin`, or in `~/.local/bin` when that directory isn't writable.

```sh
curl -fsSL https://dekka.cloud/install.sh | sh
```

Set `DEKKA_VERSION` to install a particular release, such as `v0.1.0`, and `DEKKA_INSTALL_DIR` to choose the directory. The [releases page](https://github.com/dekka-cloud/dekka-cli/releases) has archives for macOS, Linux and Windows on amd64 and arm64, each with an SBOM, and a checksums file signed with Sigstore, whose signature is `checksums.txt.sigstore.json`.

With Go installed, you can build it from source instead.

```sh
go install github.com/dekka-cloud/dekka-cli/cmd/dekka@latest
```

## Sign in

Run `dekka login`. It prints a short code and opens the dashboard, where you check that the page shows the same code and approve the sign-in for one of your organisations. The CLI then saves an API key of its own in the system keychain, which is the Keychain on macOS, the Secret Service on Linux and the Credential Manager on Windows. On a machine with no keychain, such as a server without a desktop session, it saves the key in `dekka/credentials.json` under your user config directory with permissions that let only you read it, and tells you it has done so.

```sh
dekka login
dekka whoami
```

The key is listed in the dashboard under **Settings**, then **API keys**, as "Dekka CLI on" followed by the machine's name, and revoking it there signs that machine out. Approving a sign-in makes a key, so it needs the owner or admin role in the organisation. On a machine with no browser, such as a server you reach over SSH, `dekka login --no-browser` prints the link, and you open it on another device.

To use a key you created yourself, pass it on standard input, which keeps it out of your shell history, or paste it with `dekka login --with-key`. `--token` passes it as an argument.

```sh
dekka login < dekka-key.txt
```

`dekka whoami` shows who the key acts for and which organisation it belongs to. `dekka logout` removes the saved key from this machine, and the key stays valid until you revoke it in the dashboard.

In CI, set `DEKKA_TOKEN` to a key from the dashboard instead of running `dekka login`, because it takes precedence over a saved key. `DEKKA_API_URL` points the CLI at a different Dekka API, which is only useful when you work on Dekka itself.

## Commands

| Command | What it does |
| --- | --- |
| `dekka projects list` | Lists the organisation's projects and their environments |
| `dekka apps list [--project] [--environment]` | Lists applications, workers and cron jobs with their status and address |
| `dekka deploy <service> [--branch] [--commit]` | Starts a deployment of a service |
| `dekka status <service>` | Shows a service and its five most recent deployments |
| `dekka logs <service> [--deployment <id>] [--follow]` | Prints a service's runtime logs, or one deployment's build and deploy logs |
| `dekka rollback <deployment> [--keep-current-variables] [--yes]` | Deploys an earlier deployment again |
| `dekka servers list [--project] [--environment]` | Lists virtual servers with their state, size and address |
| `dekka servers sizes` | Lists the sizes and images a new server can use, with monthly prices |
| `dekka servers create <name> --size --image --ssh-key [--wait]` | Creates a server, and with `--wait` prints how to connect once it's running |
| `dekka servers show <server>` | Shows a server's state, size, addresses and SSH command |
| `dekka servers ssh <server> [--print]` | Connects to a server over SSH, or prints the command that does |
| `dekka servers start`, `stop`, `reboot <server> [--wait]` | Starts, stops or reboots a server |
| `dekka servers resize <server> --size [--wait]` | Moves a stopped server to another size |
| `dekka servers rebuild <server> --image [--yes]` | Reinstalls a server from an image, wiping its disk |
| `dekka servers delete <server> [--yes]` | Deletes a server with its disk and snapshots |
| `dekka servers snapshots`, `snapshot <server> <name>`, `restore <server> <id> [--yes]` | Lists, takes and restores disk snapshots |
| `dekka ssh-keys list`, `add <name> <public-key-file>` | Lists the organisation's SSH keys and adds one |

A service is named by its name, slug or id. The same name can appear in several projects and environments, so `--project` and `--environment` narrow the search, and a name that still matches more than one service is refused with each match listed.

Commands print a table or a short summary, and `--output json` (or `-o json`) prints JSON with the API's field names instead. With `--output json`, `dekka logs` prints one JSON object per line.

A typical release from a terminal or a CI job looks like this.

```sh
dekka deploy api --branch main
dekka logs api --deployment <deployment id> --follow
dekka status api
```

`dekka deploy` prints the new deployment's id and the command that follows it. Following a deployment's logs streams them until the deployment finishes, and the command exits with status 1 unless it ends healthy, so a CI job can deploy and wait on the result. Following runtime logs keeps printing new lines until you press Ctrl-C.

A server is billed by the hour for as long as it exists, including while it's stopped. Creating one looks like this.

```sh
dekka ssh-keys add laptop ~/.ssh/id_ed25519.pub
dekka servers sizes
dekka servers create web-1 --project shop --size server-seed-kvm-v4 --image kvm-ubuntu-24.04 --ssh-key laptop --wait
dekka servers ssh web-1
```

`dekka servers rebuild`, `delete` and `restore` name the server and what will be lost, and ask before they call the API, as `dekka rollback` does.

`dekka rollback` deploys the image that an earlier healthy deployment ran, with the settings and the variables it ran with; `--keep-current-variables` keeps the service's variables as they are now instead. Databases, volumes and buckets are not changed, so their data and any migrations stay as they are. Before it calls the API it names the service, its project and environment, and the deployment it returns to, and asks you to confirm. `--yes` skips the question, and without a terminal the command needs `--yes`.

## Keys and permissions

The CLI has no access of its own. It sends your key with every request, and the API decides what the key may do with the same checks it applies to the dashboard, so the CLI can never reach anything the key's creator couldn't reach in the browser.

An [API key](https://docs.dekka.cloud/api-keys) belongs to one organisation, and each scope caps it at a role. The `read` scope acts as a viewer and is enough for `whoami`, `projects list`, `apps list`, `status`, `logs` and for listing and showing servers, while `write` acts as a developer and is needed for `deploy`, `rollback`, adding SSH keys and creating or changing servers. The `admin` scope manages organisation settings, which the CLI doesn't do yet, and a key with only the `ai` scope reaches the AI gateway and nothing the CLI uses. A key never does more than the person who created it, so a developer's key with `admin` still acts as a developer.

A [project token](https://docs.dekka.cloud/project-tokens) is a key that reaches one project with `read` or `write`, which makes it the safer choice for CI. It can't list the organisation's projects, so pass the project's id with `--project` when you name a service, or name the service by its id.

When a key can't see something the API answers 404, and when it can see it but not change it the API answers 403. The CLI prints the API's message with its error code and request id, which is what Dekka support needs if you ask for help.

## What comes next

`dekka plan` and `dekka apply` will preview and apply an environment described in a `dekka.toml` file once the API supports environment files, and neither command exists yet.

## Development

The API client in `internal/api` is generated with oapi-codegen from `openapi/openapi.json`, a copy of the document the public API serves at `https://api.dekka.cloud/openapi.json`. `make sync-spec` fetches it again and regenerates the client, and CI fails when regenerating from the copy changes anything. A scheduled workflow also fails when the published document no longer matches the copy.

CI runs the tests, golangci-lint and those checks on GitHub-hosted `ubuntu-latest` runners, which are free for public repositories, and a tag starting with `v` builds a release with GoReleaser on the same runners. [CONTRIBUTING.md](CONTRIBUTING.md) explains how to build and test the CLI, and [SECURITY.md](SECURITY.md) explains how to report a vulnerability.

## License

Apache License 2.0, in [LICENSE](LICENSE).
