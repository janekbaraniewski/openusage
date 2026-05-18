# Changelog

## [0.10.7](https://github.com/janekbaraniewski/openusage/compare/v0.10.6...v0.10.7) (2026-05-18)


### Features

* **claude_code:** emit plan_type from subscription state ([05faf67](https://github.com/janekbaraniewski/openusage/commit/05faf6701b5af9d595655b02e3c1a7505d882d9b))
* **config:** add hide_costs to dashboard config (both levels) ([1fcd595](https://github.com/janekbaraniewski/openusage/commit/1fcd5950b010cefe364469699b00fc64fd5560e3))
* **core:** cost-visibility policy with plan-aware auto ([9d3f342](https://github.com/janekbaraniewski/openusage/commit/9d3f342c50c1d125d3b42b7281427447225dde6f))
* **tui:** burn-rate projection and reset countdown on usage gauges ([f7ed801](https://github.com/janekbaraniewski/openusage/commit/f7ed801bf8e47854cc0199922ac07750fc9a78a2))
* **tui:** burn-rate projection on dashboard tile gauges ([d3670ee](https://github.com/janekbaraniewski/openusage/commit/d3670ee9575a4a5a2a648102fb94ccdaf88e2322))
* **tui:** c keystroke toggles hide_costs (cycles nil → true → false → nil) ([08a2b13](https://github.com/janekbaraniewski/openusage/commit/08a2b1303abb13ba7125eabd586a6a2574794ecf))
* **tui:** gate cost rendering on resolved hide_costs ([f7c5acf](https://github.com/janekbaraniewski/openusage/commit/f7c5acfff4abbf941900dba4553fc7d9bd23e477))
* **tui:** plug remaining cost-rendering leaks for hide_costs ([31681e0](https://github.com/janekbaraniewski/openusage/commit/31681e0b0741d7b4c86fa57ddd76ce35fad1c126))
* **tui:** show projected percent at reset when 100% projection exceeds window ([328b80b](https://github.com/janekbaraniewski/openusage/commit/328b80bddc743dbfce00b65979d45a4b195a342b))


### Bug Fixes

* **ci:** gofmt-align gauge_test.go happy_path case ([04862a0](https://github.com/janekbaraniewski/openusage/commit/04862a06cd1b22410ede51709b15e7e5536c3070))
* **ci:** restore permissions on refresh-release-prs job ([#157](https://github.com/janekbaraniewski/openusage/issues/157)) ([9ed0cad](https://github.com/janekbaraniewski/openusage/commit/9ed0cadef72fb406ed02101bde2954d50348cb68))
* **security:** address code-scanning alerts ([#148](https://github.com/janekbaraniewski/openusage/issues/148)) ([62e0b5b](https://github.com/janekbaraniewski/openusage/commit/62e0b5b897a14ab2eb4a2e66e77ca2ff4b47c650))
* **security:** scope workflow GITHUB_TOKEN permissions to job level ([62e0b5b](https://github.com/janekbaraniewski/openusage/commit/62e0b5b897a14ab2eb4a2e66e77ca2ff4b47c650))
* **tui:** comprehensive cost suppression when hide_costs is true ([981cb88](https://github.com/janekbaraniewski/openusage/commit/981cb88be523c6f4f9753439283979be9a707c50))
* **tui:** hide_costs fallback was leaking monetary metrics in detail summary ([9301ea0](https://github.com/janekbaraniewski/openusage/commit/9301ea01877ba208bba11462898dd3b6816dda6b))


### Dependencies

* **docs:** bump posthog-js from 1.372.10 to 1.374.1 in /docs/site in the docs-minor-and-patch group ([#153](https://github.com/janekbaraniewski/openusage/issues/153)) ([d49a468](https://github.com/janekbaraniewski/openusage/commit/d49a468461ca74decc18afd64131f43506e5f62a))
* **docs:** bump webpack-dev-server from 5.2.3 to 5.2.4 in /docs/site ([#156](https://github.com/janekbaraniewski/openusage/issues/156)) ([fdd2cee](https://github.com/janekbaraniewski/openusage/commit/fdd2ceea697a600442bfa5a407f4655f8e3340ac))
* **website:** bump puppeteer from 24.43.1 to 25.0.4 in /website ([#154](https://github.com/janekbaraniewski/openusage/issues/154)) ([ebde3f8](https://github.com/janekbaraniewski/openusage/commit/ebde3f874b98b04f6587514445c31a0889cc6143))
* **website:** bump the website-minor-and-patch group in /website with 3 updates ([#152](https://github.com/janekbaraniewski/openusage/issues/152)) ([3a110e7](https://github.com/janekbaraniewski/openusage/commit/3a110e7d5ab41d1f91c6085d0b5c5067988279a0))


### Refactoring

* **tui:** apply PR [#155](https://github.com/janekbaraniewski/openusage/issues/155) review cleanups ([b44f0b4](https://github.com/janekbaraniewski/openusage/commit/b44f0b4cec02706d265fdca45c9742a636b6dba9))

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
