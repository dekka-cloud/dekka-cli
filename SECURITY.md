# Security

If you find a vulnerability in the Dekka CLI or in Dekka itself, email security@dekka.cloud rather than opening an issue or a pull request. Include the version you used (`dekka --version`), what you did, what happened, and anything that helps reproduce it. We reply to every report and keep you updated until it is resolved.

Security fixes go into the latest release, so please upgrade before reporting a problem in an older one.

The CLI stores API keys in the system keychain, or in a file only your user can read when there is no keychain, and it sends them only to the API URL they were saved for. If you think a key has leaked, revoke it in the dashboard under **Settings**, then **API keys**, or in the project's settings for a project token.
