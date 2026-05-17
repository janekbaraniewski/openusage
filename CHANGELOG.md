# Changelog

## [0.10.6](https://github.com/janekbaraniewski/openusage/compare/v0.10.5...v0.10.6) (2026-05-17)


### Bug Fixes

* **telemetry:** cache canonical usage view, lift refresh clamp, fix daemon run flags ([92ff504](https://github.com/janekbaraniewski/openusage/commit/92ff5044ce775cff7825ed6962aac3355d3610b9))


### Dependencies

* **docs:** bump mermaid from 11.14.0 to 11.15.0 in /docs/site ([#138](https://github.com/janekbaraniewski/openusage/issues/138)) ([22b8b80](https://github.com/janekbaraniewski/openusage/commit/22b8b806988e0d69d67b1d044c3d407dd34a80fa))
* **website:** bump @protobufjs/utf8 from 1.1.0 to 1.1.1 in /website ([#141](https://github.com/janekbaraniewski/openusage/issues/141)) ([1d7340b](https://github.com/janekbaraniewski/openusage/commit/1d7340b2bb4ff38ccf04b236a2a90debe7e3fdb0))
* **website:** bump protobufjs from 7.5.5 to 7.5.8 in /website ([#143](https://github.com/janekbaraniewski/openusage/issues/143)) ([73577cd](https://github.com/janekbaraniewski/openusage/commit/73577cd95b0f838504547d5e67d891c73f41658e))
* **website:** bump the website-minor-and-patch group across 1 directory with 3 updates ([#142](https://github.com/janekbaraniewski/openusage/issues/142)) ([a5cc0c4](https://github.com/janekbaraniewski/openusage/commit/a5cc0c4f35579d01c63c9ecfc444b059b41023b0))

## [0.10.5](https://github.com/janekbaraniewski/openusage/compare/v0.10.4...v0.10.5) (2026-05-10)


### Dependencies

* align Charmbracelet x dependency updates ([#131](https://github.com/janekbaraniewski/openusage/issues/131)) ([26d4c57](https://github.com/janekbaraniewski/openusage/commit/26d4c5712ffb04f47608164262d9330503f66f9e))
* **website:** bump the website-minor-and-patch group across 1 directory with 3 updates ([#97](https://github.com/janekbaraniewski/openusage/issues/97)) ([baee92a](https://github.com/janekbaraniewski/openusage/commit/baee92ab7d3405a87a2b25a2808152137cc40f53))


### Refactoring

* PR [#95](https://github.com/janekbaraniewski/openusage/issues/95) follow-ups (cursor cleanup, zai/openrouter decomposition, TUI/daemon/logging) ([#113](https://github.com/janekbaraniewski/openusage/issues/113)) ([3761ef2](https://github.com/janekbaraniewski/openusage/commit/3761ef28d4e2e77c5b40ed6ab92784c758394d81))

## [0.10.4](https://github.com/janekbaraniewski/openusage/compare/v0.10.3...v0.10.4) (2026-05-10)


### Features

* **detect:** extract API keys from shell rc, aider config, codex auth, and keychain ([41f8252](https://github.com/janekbaraniewski/openusage/commit/41f82524ea6b1e7f3e3892486f638a3b371c22d5))
* **detect:** Tier-1 credential sources + gofmt sweep ([28ddcc7](https://github.com/janekbaraniewski/openusage/commit/28ddcc79a2603c801aa88097a945c9b730993869))


### Bug Fixes

* **detect:** silence CodeQL clear-text-logging warning on aider list parse ([9141f51](https://github.com/janekbaraniewski/openusage/commit/9141f51bbd31e9317398d636367c0487efb5747c))
* revert charmbracelet/x/ansi 0.11.7 bump — main is broken ([#109](https://github.com/janekbaraniewski/openusage/issues/109)) ([53a5149](https://github.com/janekbaraniewski/openusage/commit/53a5149125fe6979663c6df7d778ad6acb1b009d))


### Dependencies

* **deps:** bump the go-minor-and-patch group across 1 directory with 3 updates ([#96](https://github.com/janekbaraniewski/openusage/issues/96)) ([be1d03a](https://github.com/janekbaraniewski/openusage/commit/be1d03ae309f95c3e1e0a655f210da878d1c9b68))


### Refactoring

* daemon correctness fixes + provider hygiene sweep ([04b863b](https://github.com/janekbaraniewski/openusage/commit/04b863b193c61a2a52c8d0bd723fbf36411fa56e))
* **detect:** consolidate mappings, drop ExtraData duplication, fix Aider bugs ([7e68ef8](https://github.com/janekbaraniewski/openusage/commit/7e68ef8d5fdbae97fbb20510b7a1c03898ffca1c))
* **providers:** consolidate status-code switches via shared helpers ([0b9b338](https://github.com/janekbaraniewski/openusage/commit/0b9b3383a4568197c9c1fa4fcc102a80844ade70))
