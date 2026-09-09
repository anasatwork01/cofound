# Halyard - Requirements and Solution Specification

> **This is a placeholder. The canonical specification has not been committed yet.**
>
> The authoritative spec was supplied as a document rather than a file. Copy it
> here verbatim, preserving UTF-8:
>
> ```bash
> cp /path/to/your/SPEC.md /Users/apple/cofound/docs/SPEC.md
> ```
>
> It was deliberately **not** transcribed from the conversation: the copy
> received there had been through a UTF-8 → Latin-1 round trip (`§` rendered as
> `Â§`, em dashes as `â`, the architecture diagram's box-drawing characters
> destroyed). Reproducing that would bake the corruption into the repository's
> most-referenced file, and silently "correcting" it would mean rewriting a
> contract without review. See `docs/open-questions.md` Q0.
>
> Everything in this repository cites this document by section number. The
> section map below is what the rest of the repo assumes; if your copy differs,
> the citations are wrong and need updating.

## Section map

| §   | Title                                                                  |
| --- | ---------------------------------------------------------------------- |
| 0   | How to use this document                                               |
| 1   | Product scope (C1-C13, non-goals)                                      |
| 2   | Architecture overview (five planes)                                    |
| 3   | Technology stack                                                       |
| 4   | Repository layout                                                      |
| 5   | Decision log - choices already made, with reasons                      |
| 6   | Data model (control plane DDL)                                         |
| 7   | Contracts (REST surface, agent events, MCP tools, capability manifest) |
| 8   | Auth, tenancy and roles                                                |
| 9   | Sandbox orchestration (`sandboxd`)                                     |
| 10  | Git service (`gitd`)                                                   |
| 11  | The agent (opencode fork + `agentd`)                                   |
| 12  | Version control and history                                            |
| 13  | Capability system                                                      |
| 14  | Deployment and domains                                                 |
| 15  | Growth surfaces (ads, SEO)                                             |
| 16  | Credits, metering and the AI gateway                                   |
| 17  | Security model, observability and SLOs                                 |
| 18  | Frontend specification                                                 |
| 19  | Gaps not covered elsewhere (legal, operational, product, technical)    |
| 20  | Delivery phases 0-8                                                    |
| 21  | Decisions the human must make before phase 1                           |
| 22  | Facts to verify before depending on them                               |
| 23  | Definition of done for v1                                              |

`docs/mockup.html` is also referenced (SPEC §3.1, §18) as the source of the
design tokens and is likewise not yet in the repository.
