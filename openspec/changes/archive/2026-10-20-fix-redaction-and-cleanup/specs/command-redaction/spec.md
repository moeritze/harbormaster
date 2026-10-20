## Purpose

Mask secrets in command lines before harbormaster stores or prints them, since the registry and history are readable by every session.

## ADDED Requirements

### Requirement: Credential headers are masked in every flag form
Credential-bearing header values SHALL be masked whether the header follows `-H`/`--header` as a separate argument, is attached (`-HAuthorization: …`), or uses `--header=…`.

#### Scenario: Attached header flag
- **WHEN** the command is `curl -HAuthorization: Basic dXNlcjpw` or `curl --header=Authorization: Basic dXNlcjpw`
- **THEN** the stored command contains `Authorization: ***` and not the credential

### Requirement: Cookies, passwords and signatures are masked
Values SHALL be masked after `--cookie`, `-b`, `--pass`, and for any key containing `cookie` or `signature` (e.g. `X-Amz-Signature=…`).

#### Scenario: Cookie flag
- **WHEN** the command is `curl --cookie sid=abc` or `curl -b sid=abc`
- **THEN** `sid=abc` is replaced by `***`

#### Scenario: Short password flag name
- **WHEN** the command is `tool --pass hunter2`
- **THEN** `hunter2` is replaced by `***`

#### Scenario: Presigned URL
- **WHEN** an argument contains `X-Amz-Signature=deadbeef`
- **THEN** it is stored as `X-Amz-Signature=***`

### Requirement: JSON values are masked whole
A sensitive JSON string value SHALL be masked up to its closing quote, honoring backslash escapes.

#### Scenario: Escaped quote in password
- **WHEN** the body is `{"password":"x\"y"}`
- **THEN** it is stored as `{"password":"***"}`
