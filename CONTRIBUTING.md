# Contributing

Thank you for helping with the Dekka CLI. Issues and pull requests are welcome, and a short issue describing what you want to change is a good start for anything larger than a fix.

## Build and test

You need the Go version named in `go.mod`, and golangci-lint for `make lint`.

```sh
make build    # writes bin/dekka
make test     # go test -race ./...
make lint     # golangci-lint run
```

The tests run every command against an `httptest` server that stands in for the API, so they never reach the real one and need no key. A new command comes with tests that check the requests it sends and the output it prints, as a table and as JSON.

## The API client

The CLI only uses the public Dekka API, through the client that oapi-codegen generates in `internal/api` from `openapi/openapi.json`. Don't edit either file by hand. `make sync-spec` copies the document the API publishes and regenerates the client, and `make generate` regenerates it from the copy you have. CI fails when the generated client doesn't match the copy, so commit both together.

When a command needs something the API doesn't offer, the change belongs in the API first, and the CLI follows once the published document includes it.

## Pull requests

Keep each pull request to one change, and write commit messages and descriptions in complete sentences that say what changed and why. CI runs the tests, the linter, the generated-client check and a snapshot release build, and all of them need to pass before a merge.

Report security problems privately as [SECURITY.md](SECURITY.md) describes, rather than in an issue.
