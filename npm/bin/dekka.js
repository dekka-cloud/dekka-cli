#!/usr/bin/env node
"use strict";

// Runs the dekka binary for this machine, fetching it on first use.
const { spawnSync } = require("node:child_process");
const { ensure } = require("../lib/install");

ensure()
  .then((binary) => {
    const run = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
    if (run.error) throw run.error;
    process.exit(run.status ?? 1);
  })
  .catch((error) => {
    console.error(`dekka: ${error.message}`);
    process.exit(1);
  });
