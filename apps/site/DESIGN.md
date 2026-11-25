# Kandang site design

The public site at kandang.zakiego.com: the landing page now, the docs later (Fumadocs under `/docs`). This file holds the site's design contract and records design calls as they're made.

## Contract

Written before building (anti-ui-slop step 1). The finish gate checks against it.

- Audience: engineers building AI agents who want their agent to run code and make files without running it on their own machine.
- Primary task: understand in one screen what Kandang is and why it's safe, then see the one endpoint.
- Category default: a "sandbox for AI" SaaS landing with a purple gradient hero, a feature grid of six icon cards, a fake customer logo strip and "Start free".
- Predictable opposite: a README rendered as a page.
- Neither. Kandang's own idea is the pen: things go in, results come out, nothing else gets through the walls. The first screen shows one run from the agent's seat: code and a file go into a box, the box starts in milliseconds, a chart and a spreadsheet come back out, and an attempt to reach the network stays inside.
- Content rules: one headline, one sentence, one call to action, one visual. Facts only: the measured numbers from the Mac mini M4 run and the limits that held. No invented customers, users, prices or numbers. No sign-up and no pricing.
- Public copy: no people's names, home paths, internal hostnames or IPs, and no other projects of the author.
- Look: its own (no dark mono, IBM Plex or isometric mascot), not borrowed from another site. Type, spacing, buttons and motion follow the Linear reference in TASTE.md.
- States and sizes: static page, so no loading or error states. Works at 390, 768, 1024 and 1440 wide. On a phone the copy comes first, and the run demo with its caption fits in one screen.

## Decisions

- 2026-11-20, landing hero (agent's pick, not the owner's). Explore round 1 tried three heroes behind a picker. Stage: a dark framed stage with a looping run demo, where a `POST /v1/runs` request with a CSV goes into a gVisor box that starts in 28 ms, and a chart, an xlsx and stdout come back out. Blueprint: light paper with a technical drawing of the box and its walls labeled. Cage: poster type over cage bars with one giant rotating fact. The owner asked the agent to keep the variant closest to TASTE.md and the repo's decisions, so Stage was kept: it follows the Linear reference for marketing pages, gives the hero a framed stage with ornaments, a looping demo and a live number, and shows what the agent gets back from its own seat. Blueprint and Cage and the picker are deleted.
- 2026-11-25, default matplotlib chart style in the sandbox image (agent's pick, not the owner's). Three styles were rendered from the plain code a chat bot writes (bar, three lines, horizontal ranking, pie). A: light off-white, Inter, hairline grid, left title. B: dark Linear neutrals (`#0f1011`), the dataviz palette's dark steps. C: square, white, big phone-first type. The owner answered "Ok" without naming one, so B was kept: it follows the Linear reference in TASTE.md and Kandang's own dark look. It ships as `image/matplotlibrc` plus `image/sitecustomize.py` (narrower bars, clockwise pies with surface gaps) and static Inter TTFs.
