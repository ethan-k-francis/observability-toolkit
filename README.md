# observability-toolkit

Repository bootstrap for observability tooling and experiments.

## Local CI Gate

Run before every PR:

```bash
make ci
```

This runs:
- formatting and lint hooks via pre-commit
- local security scan parity (`trivy fs`)
- attribution guard checks on branch commits (and optional PR text)
