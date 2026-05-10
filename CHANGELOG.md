# Changelog

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
