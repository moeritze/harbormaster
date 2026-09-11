# Harbormaster Signed Release Pipeline Implementation Plan (Plan 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tagged releases of `harbormaster` that are reproducible, signed (Sigstore cosign, keyless), carry an SBOM and SLSA v1 build provenance, publish a Homebrew cask, and can only run after the maintainer approves the run.

**Architecture:** goreleaser v2 builds and packages; a GitHub Actions workflow on `v*` tags runs it inside a `release` environment that requires a reviewer; a second job feeds goreleaser's `checksums.txt` to the SLSA generic generator. CI validates the goreleaser config on every PR with a snapshot build so the release config never rots.

**Tech Stack:** goreleaser v2.18.1, cosign v3 (bundle signing), syft, `slsa-framework/slsa-github-generator` v2.1.0, GitHub environments.

**Spec:** `docs/superpowers/specs/2026-09-10-harbormaster-design.md` §12 (release section) and SECURITY.md supply-chain section.

## Global Constraints

- Verified against docs on 2026-09-11: goreleaser `brews.tap` is deprecated in favour of `repository`, and formula-based `brews` are being superseded by `homebrew_casks` for pre-built binaries (goreleaser deprecations page); cosign v3 signing uses `--bundle=${signature}` with `signature: "${artifact}.sigstore.json"` (goreleaser "cosign v3" post, customization/sign); SBOMs via `sboms: [{artifacts: archive}]` with syft on PATH (customization/sbom); the SLSA generic generator MUST be referenced by tag `@vX.Y.Z`, needs `actions: read`, `id-token: write`, `contents: write` on the calling job, takes `base64-subjects` in `sha256sum` format base64-encoded, and uploads provenance to the release with `upload-assets: true` (slsa-github-generator generic README).
- Every third-party action pinned to a full commit SHA with a version comment (SLSA reusable workflow is the documented exception: tag).
- Workflow top-level `permissions: contents: read`; the release job requests `contents: write` + `id-token: write`; the provenance job `actions: read` + `id-token: write` + `contents: write`.
- The release job runs in the `release` environment, which has the maintainer as required reviewer. A tag push therefore waits for approval in the Actions UI.
- No stored signing keys. The only secret is `HOMEBREW_TAP_TOKEN` (fine-grained, `homebrew-tap` contents: write); when absent the cask upload is skipped and the release still succeeds.
- ldflags set `github.com/moeritze/harbormaster/internal/cli.Version={{.Version}}`; `-trimpath`; `CGO_ENABLED=0`.
- Targets: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64 (best effort per spec §3).
- Commits: conventional prefix, single subject, trailer `Co-Authored-By: Claude Code <noreply@anthropic.com>`; never a `Claude-Session:` line. Work in `~/repositories/harbormaster-wt/release` on `feat/release-pipeline`; merge via PR.

---

## File Structure

```
.goreleaser.yaml                     build/archive/checksum/sbom/sign/cask/changelog
.github/workflows/release.yml        tag-triggered release + provenance (environment-gated)
.github/workflows/ci.yml             + goreleaser-check job (config check + snapshot, no publish)
Makefile                             + release-dry target
README.md                            Install section rewritten (brew, go install @tag, verification)
SECURITY.md                          Supply chain section: what a release actually ships
```

---

### Task 1: goreleaser config + local dry run

**Files:** Create `.goreleaser.yaml`; modify `Makefile`, `.gitignore` (already has `dist/`).

- [ ] **Step 1: Write `.goreleaser.yaml`**

```yaml
version: 2

project_name: harbormaster

before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - id: harbormaster
    main: ./cmd/harbormaster
    binary: harbormaster
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      - -s -w -X github.com/moeritze/harbormaster/internal/cli.Version={{.Version}}
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: windows
        goarch: arm64
    mod_timestamp: "{{ .CommitTimestamp }}"

archives:
  - id: default
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md
      - SECURITY.md

checksum:
  name_template: checksums.txt
  algorithm: sha256

sboms:
  - id: archives
    artifacts: archive

signs:
  - id: checksum
    cmd: cosign
    signature: "${artifact}.sigstore.json"
    args:
      - sign-blob
      - "--bundle=${signature}"
      - "${artifact}"
      - "--yes"
    artifacts: checksum
    output: true

homebrew_casks:
  - name: harbormaster
    binaries: [harbormaster]
    description: Registry of local dev servers for multi-session, multi-worktree development
    homepage: https://github.com/moeritze/harbormaster
    license: MIT
    repository:
      owner: moeritze
      name: homebrew-tap
      branch: main
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    skip_upload: "{{ if .Env.HOMEBREW_TAP_TOKEN }}false{{ else }}true{{ end }}"
    commit_author:
      name: goreleaser
      email: noreply@goreleaser.com
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/harbormaster"]
          end

changelog:
  sort: asc
  use: github
  groups:
    - title: Features
      regexp: '^.*?feat(\(.+\))?!?:.+$'
      order: 0
    - title: Fixes
      regexp: '^.*?fix(\(.+\))?!?:.+$'
      order: 1
    - title: Performance
      regexp: '^.*?perf(\(.+\))?!?:.+$'
      order: 2
    - title: Other
      order: 999
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"
      - "^chore:"

release:
  github:
    owner: moeritze
    name: harbormaster
  draft: false
  prerelease: auto
  mode: keep-existing
  footer: |
    ## Verify

    ```sh
    cosign verify-blob --bundle checksums.txt.sigstore.json \
      --certificate-identity-regexp '^https://github.com/moeritze/harbormaster/' \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
    sha256sum -c checksums.txt --ignore-missing
    ```
```

- [ ] **Step 2: Makefile target**

```make
release-dry:
	go run github.com/goreleaser/goreleaser/v2@v2.18.1 release --snapshot --clean --skip=publish,sign,sbom
release-check:
	go run github.com/goreleaser/goreleaser/v2@v2.18.1 check
```
Add both to `.PHONY`.

- [ ] **Step 3: Verify locally**

Run: `make release-check && make release-dry && ls dist/ | head` (set `HOMEBREW_TAP_TOKEN=` empty in the environment so the template resolves). Expected: `dist/checksums.txt`, five archives, a `homebrew/Casks/harbormaster.rb` (skipped upload), no errors. `./dist/harbormaster_darwin_arm64*/harbormaster version` prints the snapshot version.

- [ ] **Step 4: Commit** `feat(release): goreleaser config with signing, sbom, cask`

---

### Task 2: release workflow + CI check job

**Files:** Create `.github/workflows/release.yml`; modify `.github/workflows/ci.yml`.

- [ ] **Step 1: `.github/workflows/release.yml`**

```yaml
name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: read

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    environment: release
    permissions:
      contents: write
      id-token: write
    outputs:
      hashes: ${{ steps.hashes.outputs.hashes }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - uses: sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6 # v4.1.2
      - uses: anchore/sbom-action/download-syft@3ad7283483fc7af8ff2b4ea19663c2d5ca935e26 # v0.24.2
      - uses: goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7.2.3
        with:
          distribution: goreleaser
          version: v2.18.1
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
      - name: Collect subjects for provenance
        id: hashes
        run: echo "hashes=$(base64 -w0 < dist/checksums.txt)" >> "$GITHUB_OUTPUT"

  provenance:
    needs: [goreleaser]
    permissions:
      actions: read
      id-token: write
      contents: write
    uses: slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@v2.1.0
    with:
      base64-subjects: ${{ needs.goreleaser.outputs.hashes }}
      provenance-name: harbormaster.intoto.jsonl
      upload-assets: true
```

- [ ] **Step 2: CI job in `ci.yml`** (after `lint`):

```yaml
  goreleaser-check:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@e14015d583714f6e62063499dc959a02595150a1 # v2.21.1
        with:
          egress-policy: audit
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7.2.3
        with:
          distribution: goreleaser
          version: v2.18.1
          args: check
      - uses: goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7.2.3
        with:
          distribution: goreleaser
          version: v2.18.1
          args: release --snapshot --clean --skip=publish,sign,sbom
        env:
          HOMEBREW_TAP_TOKEN: ""
```

- [ ] **Step 3: Validate YAML, commit** `ci(release): tag-triggered signed release with SLSA provenance; goreleaser check in CI`

---

### Task 3: Docs + repo settings

- [ ] README Install section: brew tap (`brew install moeritze/tap/harbormaster`), `go install …@v0.1.0`, verification block (cosign verify-blob with identity regexp + OIDC issuer, `slsa-verifier verify-artifact`), note that `@main` is unsigned.
- [ ] SECURITY.md supply-chain section: what a release ships (checksums, cosign bundle, SBOMs per archive, SLSA v1 provenance, environment-gated workflow, no stored keys) and how to verify.
- [ ] Repo: `release` environment with the maintainer as required reviewer; public `moeritze/homebrew-tap` repo; user creates `HOMEBREW_TAP_TOKEN`.
- [ ] Commit `docs: install from signed releases; supply-chain section`.

---

### Task 4: Ship

- [ ] PR (draft → CI → ready → auto-merge rebase). After merge: `git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0`; the run waits at the `release` environment for approval; verify after approval: release assets, `checksums.txt.sigstore.json`, `*.sbom.json`, `harbormaster.intoto.jsonl`, cask in the tap (only if the secret exists).
