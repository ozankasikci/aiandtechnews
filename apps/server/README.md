# Node server

## Executable HTTP contracts

The retained Node API is snapshotted as executable JSON contracts in
[`contracts/node/contracts.json`](contracts/node/contracts.json). The capture always launches the
isolated `scripts/contract-server.ts`; it never reads an existing database or uploads directory.

From this package:

```sh
pnpm contract:check       # fresh capture and byte comparison; never writes
pnpm contract:capture     # atomically accept a reviewed fresh baseline
pnpm contract:server      # low-level isolated server harness
```

Run `contract:check` normally and in CI. Run `contract:capture` only after reviewing an intentional
Node behavior change. See [`contracts/README.md`](contracts/README.md) for fixture scope and safety
rules.
