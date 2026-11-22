# dekka

`dekka` deploys and runs applications on [Dekka](https://dekka.cloud) from your terminal. This package runs the same binary as the other installs: on first use it downloads the release for your machine from [dekka-cloud/dekka-cli](https://github.com/dekka-cloud/dekka-cli), checks it against the release's checksums, which are published inside this package, and keeps it for the next run.

```sh
npx dekka login
npx dekka deploy api --branch main
```

`dekka login` opens the dashboard, where you approve the sign-in. The [CLI's README](https://github.com/dekka-cloud/dekka-cli#readme) covers every command.
