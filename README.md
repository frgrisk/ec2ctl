# ec2ctl

[![Go Report Card](https://goreportcard.com/badge/github.com/frgrisk/ec2ctl)](https://goreportcard.com/report/github.com/frgrisk/ec2ctl)

Your EC2 fleet, at a glance: find instances, bring them online, and power down when you are done.

Install with:

```bash
go install github.com/frgrisk/ec2ctl@latest
```

Try the interface without an AWS account or any network requests:

```bash
ec2ctl demo
```

See your fleet:

```bash
ec2ctl status --regions us-east-1
ec2ctl ls --tag Environment=dev
ec2ctl status --wide
ec2ctl status -o json
```

The fleet view shows totals and color-coded states, grouped by region. Narrow terminals switch to stacked instance cards; `--wide` shows full names, availability zones, lifecycle, spot type, and hibernation details. Set `NO_COLOR=1` to disable color or `ACCESSIBLE=1` for plain output and accessible confirmation prompts. Redirected output has no animation or color.

JSON output preserves the region/instance structure, returns `[]` for an empty fleet, and keeps progress and errors out of stdout. Failed queries exit nonzero instead of reporting an empty fleet. Regions are sorted, and all pages of matching instances are retrieved.

Start and stop instances interactively:

```bash
ec2ctl start --regions us-east-1 --tag Environment=dev
ec2ctl stop --regions us-east-1 --tag Environment=dev
```

For scripts and agent sessions, use `--force` (or `-f`) to skip confirmation:

```bash
ec2ctl start --regions us-east-1 --tag Environment=dev --force
ec2ctl stop --regions us-east-1 --tag Environment=dev --force
ec2ctl terminate --regions us-east-1 i-0123456789abcdef0 --force
```

All three commands require `--force` when standard input is not a terminal. Without it, they exit with status 1 before making any AWS requests and print an error naming the flag. Piping `yes` or setting `ACCESSIBLE=1` does not bypass this requirement. With terminal input and no flag, confirmation is still required.

Instance IDs are optional for `status`, `start`, and `stop`; without IDs these commands use the region and tag filters. With no region filter, all enabled regions are searched. `modify` and `terminate` require explicit IDs. Invalid IDs are rejected before contacting AWS.

Lifecycle output reports that a request was accepted; EC2 may still be transitioning. Run `ec2ctl status` with the same filters to check the current state. Failed actions exit nonzero; if some regions succeeded before another failed, their results are printed.

`--force` only skips confirmation. It does not change instance selection, bypass AWS permissions, or request an EC2 forced stop.

## Tests

Run unit tests with `go test ./...`.

Run the lifecycle integration tests from the repository root with Docker and Go installed:

```bash
./scripts/test-emulator.sh
```

This builds AWS Emulator from the latest published package, starts an isolated container on loopback with telemetry off, and runs the detached and real-terminal lifecycle tests with the race detector. The tests use emulator credentials, create disposable EC2 instances, and verify their states after each command. A forwarding proxy counts CLI requests so refusal tests also assert that no AWS requests were sent. The container, network, and generated credentials are removed when the runner exits.

The same runner executes in the **AWS Emulator lifecycle tests** CI job on pull requests and pushes to `main`. It requires no AWS account or repository secrets. Set `EC2CTL_EMULATOR_PORT` to use a port other than the default `9297`. Direct `go test -tags=integration ./cmd` runs require `EC2CTL_EMULATOR_DIR` and `EC2CTL_EMULATOR_ENDPOINT` from a running emulator; missing configuration fails the tests instead of skipping them.
