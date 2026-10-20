## Decisions

- **Over-redact `-b`.** `-b` means cookie only for curl; elsewhere (`git checkout -b name`) its value gets masked too. Accepted: redaction fails safe by design (see `sensitiveKey` comment).
- **`pass\b` not `pass`.** `pass` as a substring would mask `--passthrough`, `--compass`; `\b` keeps `--pass`/`pass=` while `password`/`passwd` are already covered.
- **Attached headers:** in `redactArg`, strip a leading `-H` or `--header=` and apply `authHeader` to the rest before the assignment rule (which would otherwise split `--header=Authorization: Basic x` on spaces).
- **`--path-format=absolute`** (git ≥ 2.31, 2021) over joining onto `dir`: one flag, no cwd reasoning. Older git fails `rev-parse` → `Discover` returns an empty Context (existing degrade path).

## Risks / Trade-offs

- [Users on git < 2.31 lose worktree identity] → falls back to cwd keying, same as outside git; README notes the minimum.
