# ec2ctl

[![Go Report Card](https://goreportcard.com/badge/github.com/frgrisk/ec2ctl)](https://goreportcard.com/report/github.com/frgrisk/ec2ctl)

Install with:

```bash
go install github.com/frgrisk/ec2ctl@latest
```

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

`--force` only skips confirmation. It does not change instance selection, bypass AWS permissions, or request an EC2 forced stop.
