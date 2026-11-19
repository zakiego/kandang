# Security policy

Kandang runs untrusted code, so sandbox escapes and anything that gets past its limits matter a lot. Thanks for looking.

## Reporting a vulnerability

Please don't open a public issue. Report it privately through GitHub instead:

1. Go to the [Security tab](https://github.com/zakiego/kandang/security) of this repo.
2. Click "Report a vulnerability" and fill in the form.

Include what you ran, what you expected, what happened, and the Kandang and `runsc` versions (`GET /health` shows both). A proof of concept helps a lot.

You'll get a reply within 7 days. Once a fix is out, the advisory is published with credit to you, unless you'd rather stay anonymous.

## Scope

In scope:

- Escaping the gVisor sandbox or reaching the host from inside a run
- Reaching the network from inside a run
- Getting around the memory, CPU, process, file size or timeout limits
- Reading or writing host files through input or output file names, symlinks or anything else
- Calling `POST /v1/runs` without the token, or learning the token

Out of scope:

- Bugs in gVisor itself. Report those to the [gVisor project](https://github.com/google/gvisor/security).
- Denial of service by someone who already holds a valid token. The queue and the per-run limits are the defense there.
- The marketing site and docs, unless they expose something sensitive.

## Supported versions

Only the latest commit on `main` gets fixes.
