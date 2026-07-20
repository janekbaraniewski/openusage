# Changelog

## 0.24.0 (2026-07-20)

<!-- Release notes generated using configuration in .github/release.yml at main -->

## What's Changed
### Dependencies
* deps(deps): bump github.com/klauspost/compress from 1.18.6 to 1.19.0 in the go-minor-and-patch group by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/244
* ci(actions): bump the actions-minor-and-patch group with 6 updates by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/247
* deps(website): bump the website-minor-and-patch group in /website with 3 updates by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/246
* deps(docs): bump posthog-js from 1.396.0 to 1.396.7 in /docs/site in the docs-minor-and-patch group by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/245
* deps(deps): bump the go-minor-and-patch group with 4 updates by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/252
* deps(docs): bump the docs-minor-and-patch group in /docs/site with 8 updates by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/254
* deps(website): bump the website-minor-and-patch group in /website with 2 updates by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/253
* ci(actions): bump the actions-minor-and-patch group with 7 updates by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/256
* deps(docs): bump typescript from 6.0.3 to 7.0.2 in /docs/site by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/255
* deps(docs): bump websocket-driver from 0.7.4 to 0.7.5 in /docs/site by @dependabot[bot] in https://github.com/janekbaraniewski/openusage/pull/267
### Other Changes
* fix(docs): stop extending @docusaurus/tsconfig so typecheck works on typescript 7 by @janekbaraniewski in https://github.com/janekbaraniewski/openusage/pull/261
* feat(claude_code): OAuth usage-API fallback so the 5h/7d gauge works off-macOS by @PavelCz in https://github.com/janekbaraniewski/openusage/pull/249
* feat(release): credit contributors in release notes by @janekbaraniewski in https://github.com/janekbaraniewski/openusage/pull/262
* refactor(claude_code): unify usage-API auth sources behind a closure list by @roehrijn in https://github.com/janekbaraniewski/openusage/pull/258
* feat(themes): add Catppuccin Frappé by @achappell in https://github.com/janekbaraniewski/openusage/pull/257
* fix(config): follow symlink chain when saving config by @roehrijn in https://github.com/janekbaraniewski/openusage/pull/264
* feat: add Codex credit forecasting by @achappell in https://github.com/janekbaraniewski/openusage/pull/263
* fix(tui): surface browser-session keybindings in the Keys tab footer by @achappell in https://github.com/janekbaraniewski/openusage/pull/251

## New Contributors
* @PavelCz made their first contribution in https://github.com/janekbaraniewski/openusage/pull/249
* @roehrijn made their first contribution in https://github.com/janekbaraniewski/openusage/pull/258
* @achappell made their first contribution in https://github.com/janekbaraniewski/openusage/pull/257

**Full Changelog**: https://github.com/janekbaraniewski/openusage/compare/v0.23.0...v0.24.0

## [0.23.0](https://github.com/janekbaraniewski/openusage/compare/v0.22.0...v0.23.0) (2026-07-05)


### Features

* **azure:** adopt OpenCode env vars and auto-link OpenCode Azure usage ([26050fe](https://github.com/janekbaraniewski/openusage/commit/26050fe3ca2f858017cd261e69ce528afbdad655))
* **providers:** add Azure OpenAI Service provider ([#1](https://github.com/janekbaraniewski/openusage/issues/1)) ([38f6a90](https://github.com/janekbaraniewski/openusage/commit/38f6a90b3b4b118441f290f02cea4f90aed70fac))


### Bug Fixes

* **azure:** address PR review — t.Setenv in tests, sync provider counts ([2abd452](https://github.com/janekbaraniewski/openusage/commit/2abd452321f8e6e0cc4d19f0bd1ae07ba5a1e258))

## [0.22.0](https://github.com/janekbaraniewski/openusage/compare/v0.21.0...v0.22.0) (2026-06-30)


### Features

* **config:** raise the retention ceiling from 90d to ~10y ([1f32f6e](https://github.com/janekbaraniewski/openusage/commit/1f32f6e71eadde7dd133024bcd8380b07840ac82))
* **metrics:** add cache hit ratio across providers ([10cbbd9](https://github.com/janekbaraniewski/openusage/commit/10cbbd9e03d96713f89ce9c3509b774b2ffbf32f))
* **metrics:** cache hit ratio across providers ([#213](https://github.com/janekbaraniewski/openusage/issues/213)) ([10cbbd9](https://github.com/janekbaraniewski/openusage/commit/10cbbd9e03d96713f89ce9c3509b774b2ffbf32f))
* **telemetry:** downsample-and-keep — daily rollup + prune-after-rollup ([4bfb5fd](https://github.com/janekbaraniewski/openusage/commit/4bfb5fd55167f464f107e3d8ad889d8bc54de546))


### Bug Fixes

* **daemon:** stop the launchd restart loop on macOS ([903e2a2](https://github.com/janekbaraniewski/openusage/commit/903e2a2222829aee60ef5c60b77b8e42fd5705d0))
* **homebrew:** surface tap-trust requirement to stop unlinked keg ([#221](https://github.com/janekbaraniewski/openusage/issues/221)) ([92aacbf](https://github.com/janekbaraniewski/openusage/commit/92aacbfc0e1332d41b70e7335da0c6b73bf3b56a)), closes [#216](https://github.com/janekbaraniewski/openusage/issues/216)
* **telemetry:** make retention actually bound the database ([3e61f41](https://github.com/janekbaraniewski/openusage/commit/3e61f412a4026ab55b3c81796769fccc42f0d7f5))
* **tmux:** keep the 5h usage quota visible on the status bar ([044b247](https://github.com/janekbaraniewski/openusage/commit/044b24758021e3084a27110ec72cdf02992a7c26))


### Performance

* **telemetry:** open the read model read-only ([b30047a](https://github.com/janekbaraniewski/openusage/commit/b30047aa9c348c47dd1f0142a04a8867cab6b2a6))


### Dependencies

* **deps:** bump the go-minor-and-patch group across 1 directory with 5 updates ([#233](https://github.com/janekbaraniewski/openusage/issues/233)) ([008b098](https://github.com/janekbaraniewski/openusage/commit/008b098b1ed339d6a19bbfb00e8847f32744210c))
* **docs:** bump dompurify from 3.4.2 to 3.4.11 in /docs/site ([#228](https://github.com/janekbaraniewski/openusage/issues/228)) ([057ebf7](https://github.com/janekbaraniewski/openusage/commit/057ebf711f5340f644b3d749402a6ab08eee2ce7))
* **docs:** bump launch-editor from 2.13.2 to 2.14.1 in /docs/site ([#225](https://github.com/janekbaraniewski/openusage/issues/225)) ([0178d1b](https://github.com/janekbaraniewski/openusage/commit/0178d1bb81f24b6d6095699c1e49dbe99f3a90fc))
* **docs:** bump posthog-js from 1.382.0 to 1.386.6 in /docs/site in the docs-minor-and-patch group ([#217](https://github.com/janekbaraniewski/openusage/issues/217)) ([04e3daf](https://github.com/janekbaraniewski/openusage/commit/04e3daf4d141c9cddf220f64c9ed65dbf69c419e))
* **docs:** bump posthog-js from 1.386.6 to 1.391.6 in /docs/site in the docs-minor-and-patch group ([#231](https://github.com/janekbaraniewski/openusage/issues/231)) ([aeaeaeb](https://github.com/janekbaraniewski/openusage/commit/aeaeaeb48c9091a3c16b1e38eae1b2d93541e921))
* **docs:** bump posthog-js from 1.391.6 to 1.396.0 in /docs/site in the docs-minor-and-patch group ([#235](https://github.com/janekbaraniewski/openusage/issues/235)) ([8c5d20f](https://github.com/janekbaraniewski/openusage/commit/8c5d20f827fb68cde81025a9acf1e63996e63494))
* **docs:** bump undici from 7.25.0 to 7.28.0 in /docs/site ([#227](https://github.com/janekbaraniewski/openusage/issues/227)) ([e64bb4d](https://github.com/janekbaraniewski/openusage/commit/e64bb4ddff67a82c1f4420fb0c0de6994c3c40ac))
* **docs:** bump webpack-dev-server from 5.2.4 to 5.2.5 in /docs/site ([#230](https://github.com/janekbaraniewski/openusage/issues/230)) ([36fbee6](https://github.com/janekbaraniewski/openusage/commit/36fbee679acefbaabbda87d3f2e18c3a7d138744))
* **docs:** bump ws in /docs/site ([#226](https://github.com/janekbaraniewski/openusage/issues/226)) ([13a3df6](https://github.com/janekbaraniewski/openusage/commit/13a3df66544ef5826de6d73a90a87606ae0914a9))
* **website:** bump dompurify from 3.4.0 to 3.4.10 in /website ([#223](https://github.com/janekbaraniewski/openusage/issues/223)) ([c752ba4](https://github.com/janekbaraniewski/openusage/commit/c752ba430002b627dafb7e7202e0451cea24b706))
* **website:** bump dompurify from 3.4.10 to 3.4.11 in /website ([#229](https://github.com/janekbaraniewski/openusage/issues/229)) ([ae40f28](https://github.com/janekbaraniewski/openusage/commit/ae40f2882ed871d6c5689e42ee6a2d2eb2be50da))
* **website:** bump the website-minor-and-patch group across 1 directory with 4 updates ([#237](https://github.com/janekbaraniewski/openusage/issues/237)) ([bca2578](https://github.com/janekbaraniewski/openusage/commit/bca257858901df0e6d1891ca0d25215fd2ad800c))

## [0.21.0](https://github.com/janekbaraniewski/openusage/compare/v0.20.0...v0.21.0) (2026-06-11)


### Features

* **windows:** first-class daemon lifecycle + integrations hooks parity ([4c5b7b9](https://github.com/janekbaraniewski/openusage/commit/4c5b7b9d74322b405fb9d1968235697512785fd7))


### Bug Fixes

* **detect:** detect opencode auth.json on Windows (XDG-style path) ([c02535b](https://github.com/janekbaraniewski/openusage/commit/c02535bf77038a3ecae15c6c8cb23eb7b51fe1cc)), closes [#149](https://github.com/janekbaraniewski/openusage/issues/149)
* **windows:** correct path resolution across detect, telemetry, pricing, integrations ([1a57522](https://github.com/janekbaraniewski/openusage/commit/1a5752228b515ed9ce2af80576b62853f1241704))


### Dependencies

* **docs:** bump shell-quote from 1.8.3 to 1.8.4 in /docs/site ([b0955b8](https://github.com/janekbaraniewski/openusage/commit/b0955b8ccea3418415445f27017f31cb80fdc1e1))

## [0.20.0](https://github.com/janekbaraniewski/openusage/compare/v0.19.1...v0.20.0) (2026-06-09)


### Features

* **demo:** show fewer breakdown entities for narrow time windows ([b73a4a4](https://github.com/janekbaraniewski/openusage/commit/b73a4a4df31082cc34fd83de8daf7c7505726a4e))
* **statusline:** add 5h usage-window % segment (sourced from the daemon) ([e900f2b](https://github.com/janekbaraniewski/openusage/commit/e900f2b9ebf574de5d19894d84e67231c1349833))
* **statusline:** interactive installer + docs for the Claude Code statusline ([0ea9cb2](https://github.com/janekbaraniewski/openusage/commit/0ea9cb25a446f5219128cfe0f34cc4f87916b2b1))


### Bug Fixes

* **demo:** hide empty breakdown sections so detail views aren't full of placeholders ([aa7ca5d](https://github.com/janekbaraniewski/openusage/commit/aa7ca5dc77d8be176034fb87ceee7b7b80ad58cd))
* **demo:** make the demo respond to the selected time window ([866ed18](https://github.com/janekbaraniewski/openusage/commit/866ed18925c7ea5d04f100aa0cf81572f6f1e2ec))
* **demo:** prune all breakdown dimensions for narrow windows ([09a1fbf](https://github.com/janekbaraniewski/openusage/commit/09a1fbf09400052e561a0373072e16ce5d028dc2))
* **demo:** remove bogus tool/language data from the openrouter snapshot ([71f0375](https://github.com/janekbaraniewski/openusage/commit/71f03755a542f2ff9631b766bc4b11483062b6c7))
* **demo:** scale all breakdown sections with the time window, not just the header ([9ee8d67](https://github.com/janekbaraniewski/openusage/commit/9ee8d67108a88782fa921473ba5b4b6e1c18a6f7))


### Performance

* **statusline:** cache the 5h usage % and skip log parsing when unneeded ([2e05407](https://github.com/janekbaraniewski/openusage/commit/2e05407327238a485ad7f43da7ef112e09f1cd34))


### Refactoring

* **statusline:** make install/uninstall subcommands (align with tmux) ([ef1d1bf](https://github.com/janekbaraniewski/openusage/commit/ef1d1bfb08f09c23eab36aecb36d22781107fc8d))

## [0.19.1](https://github.com/janekbaraniewski/openusage/compare/v0.19.0...v0.19.1) (2026-06-08)


### Bug Fixes

* **icons:** real logos for droid/mux/pi; drop crush/codebuff tiles ([15019bd](https://github.com/janekbaraniewski/openusage/commit/15019bd88f167e3a24c2d9476e303111f6ddb405))

## [0.19.0](https://github.com/janekbaraniewski/openusage/compare/v0.18.0...v0.19.0) (2026-06-08)


### Features

* **seo:** advertise all 34 providers and every feature across site, docs, llms ([0ad1253](https://github.com/janekbaraniewski/openusage/commit/0ad12538d94e892759825adb58f1af2a2a51c395))
* **web:** add "in your status bar" section to the landing page ([a91f6d9](https://github.com/janekbaraniewski/openusage/commit/a91f6d90a50ba2b06733d1d5e36863a9b5b613c3))

## [0.18.0](https://github.com/janekbaraniewski/openusage/compare/v0.17.0...v0.18.0) (2026-06-08)


### Features

* **tmux:** add provider logos for 10 more tools, distinct emoji for the rest ([4f6cea8](https://github.com/janekbaraniewski/openusage/commit/4f6cea8c81d51048dfeac4e5344b9b968243fb41))
* **tmux:** redesign custom step as a component builder with live preview ([02f6800](https://github.com/janekbaraniewski/openusage/commit/02f68003b613658133c4671f84a21fa1ab87f838))
* **tmux:** single-screen live-preview install configurator ([745ac98](https://github.com/janekbaraniewski/openusage/commit/745ac988ef0c4d4af85c14e9ae5e4c1b08896d8d))
* **tmux:** wizard can customize the template interactively ([51ee71f](https://github.com/janekbaraniewski/openusage/commit/51ee71fd2dac5e472fb76fe20fe93d6c1b98f9f8))
* **tmux:** wizard configures dynamic / pinned / multiple providers ([86a728f](https://github.com/janekbaraniewski/openusage/commit/86a728fe147363a5d0e3df3adafbafb1ba57272c))


### Bug Fixes

* **tmux:** configurator preview honors the icons choice ([bac38a5](https://github.com/janekbaraniewski/openusage/commit/bac38a527d6e5e68cc4950c99f3616380a686e05))
* **tmux:** go.mod tidy + bigger, centered provider icons ([d4f4983](https://github.com/janekbaraniewski/openusage/commit/d4f49835732227aa421f9d7f8028e30dd671cf4d))
* **tmux:** reserve a trailing column after custom-font logos ([a3e69d4](https://github.com/janekbaraniewski/openusage/commit/a3e69d486b2cdd4bfe71cde5ae085bd018f762d0))

## [0.17.0](https://github.com/janekbaraniewski/openusage/compare/v0.16.0...v0.17.0) (2026-06-08)


### Features

* **tmux:** 'font setup' auto-configures per-range fallback (preferred path) ([e627dd5](https://github.com/janekbaraniewski/openusage/commit/e627dd5dd30583ddd8213675c698a233255bbf4f))
* **tmux:** add provider icon font (glyph tier + generation + release) ([8957230](https://github.com/janekbaraniewski/openusage/commit/8957230969326902f9ebaef0f908d1c857af38c6))
* **tmux:** add terminal-font augmenter (copy + extend, never modify original) ([ebcc8e5](https://github.com/janekbaraniewski/openusage/commit/ebcc8e5294226f99a3237394faa1f1a2b93799a9))
* **tmux:** font CLI command, install prompt, auto-upgrade glyphs ([3a593be](https://github.com/janekbaraniewski/openusage/commit/3a593be944cb68136d5c9f632ac4fde8ab8ce55a))
* **tmux:** font patch command + reliable macOS font detection + docs ([7dac3a9](https://github.com/janekbaraniewski/openusage/commit/7dac3a9393922f4a51619add61db30d58fb66df0))
* **tmux:** maximize icon size (true ink bounds) + inject a separator ([86af54c](https://github.com/janekbaraniewski/openusage/commit/86af54cbafefb81419b4c5a7e054bb3510d98efe))
* **tmux:** one-stop interactive install wizard ([ec7bd8b](https://github.com/janekbaraniewski/openusage/commit/ec7bd8b8201635914bd613dc51002bda6e5e31a4))
* **tmux:** raise patched-font icon width cap (1.8 -&gt; 2.0) ([3818198](https://github.com/janekbaraniewski/openusage/commit/3818198d063687884da160b13b7e28fd54adf48e))
* **tmux:** scale provider icons to fill the full character height ([1705e8a](https://github.com/janekbaraniewski/openusage/commit/1705e8a20f37cb44cd12980ad779c831f4d249a8))
* **tmux:** tint the provider icon with its brand color ([d9ea1a3](https://github.com/janekbaraniewski/openusage/commit/d9ea1a332c76236fa47292695516553b1720b690))


### Bug Fixes

* **telemetry:** today_api_cost is today-scoped, not the view-window total ([cb801bd](https://github.com/janekbaraniewski/openusage/commit/cb801bd80c17893536514b7f8b59ebad4ae8e8ff))
* **tmux:** add zai + moonshot glyphs to ascii/unicode tiers ([57ba26f](https://github.com/janekbaraniewski/openusage/commit/57ba26fcaef347b6df4b059afcb92a85f7403216))
* **tmux:** give Gemini a distinct unicode glyph (was generic sparkles) ([3c0d98f](https://github.com/janekbaraniewski/openusage/commit/3c0d98f824c0934a7969af003498d227f829c930))
* **tmux:** make icon font generation deterministic ([41d92a7](https://github.com/janekbaraniewski/openusage/commit/41d92a7f9ad07f66718bb1f5ca4049e21b0cec6d))
* **tmux:** review fixes — strategy-keyed cache, nerdfont fallback, cleanups ([a00cc8b](https://github.com/janekbaraniewski/openusage/commit/a00cc8ba7c8ee0a0d4333cb4153418627d67b681))
* **tmux:** stop status-bar flicker, clarify default, skip data-less tools ([f4ed30f](https://github.com/janekbaraniewski/openusage/commit/f4ed30f00cf78b9831640a0fbe201e0f14714c12))


### Dependencies

* **deps:** bump github.com/mattn/go-sqlite3 from 1.14.44 to 1.14.45 in the go-minor-and-patch group ([#187](https://github.com/janekbaraniewski/openusage/issues/187)) ([808d881](https://github.com/janekbaraniewski/openusage/commit/808d881ae65f7396517be3b88a6875b92840bfeb))
* **docs:** bump the docs-minor-and-patch group in /docs/site with 2 updates ([#190](https://github.com/janekbaraniewski/openusage/issues/190)) ([5b7a1db](https://github.com/janekbaraniewski/openusage/commit/5b7a1db74ff0a92da1a2023fc410d53e558dae22))
* **website:** bump posthog-js from 1.378.1 to 1.382.0 in /website in the website-minor-and-patch group ([#188](https://github.com/janekbaraniewski/openusage/issues/188)) ([42329a2](https://github.com/janekbaraniewski/openusage/commit/42329a282a1b127b6c6cdd3284bc43d7294e520e))


### Refactoring

* **tmux:** share font script code, harden patcher, guard font in CI ([60120bb](https://github.com/janekbaraniewski/openusage/commit/60120bbdb1fb05acd543cfcaa034c3d2efe22c49))
* **tmux:** split platform-specific font detection via build constraints ([4ea0a7a](https://github.com/janekbaraniewski/openusage/commit/4ea0a7aa1d00eef1bed59243510c4891fd1099af))
* **tmux:** unify the managed-sentinel-block logic ([1c6e1d3](https://github.com/janekbaraniewski/openusage/commit/1c6e1d3dba31034ff5c59363e03a0d98904cfe2b))
* **tmux:** use samber/lo to match codebase conventions ([5a1054a](https://github.com/janekbaraniewski/openusage/commit/5a1054adbe7e1bacb00128f34505a1575620e7a4))

## [0.16.0](https://github.com/janekbaraniewski/openusage/compare/v0.15.1...v0.16.0) (2026-06-08)


### Features

* **seo:** strengthen AI-search positioning (LLMSO/AEO/GEO) ([ea7d188](https://github.com/janekbaraniewski/openusage/commit/ea7d1882a6e80615ce8c288aac3c4547a70f2cfd))


### Bug Fixes

* **seo:** homepage operatingSystem includes Windows (matches docs + FAQ) ([54ad7bf](https://github.com/janekbaraniewski/openusage/commit/54ad7bf102f5db2a594ec150c74338ec7db5f419))
* **seo:** reference docs sitemap from root robots + key commands in llms-full ([fb75935](https://github.com/janekbaraniewski/openusage/commit/fb75935568ac95703eb366e5ab5dc0cfadc42fc5))

## [0.15.1](https://github.com/janekbaraniewski/openusage/compare/v0.15.0...v0.15.1) (2026-06-08)


### Bug Fixes

* **tmux:** prepend status segment to inner edge of status-right ([6c809fe](https://github.com/janekbaraniewski/openusage/commit/6c809fe6b6494308ca44a673b4d3fdda55874bcd))

## [0.15.0](https://github.com/janekbaraniewski/openusage/compare/v0.14.0...v0.15.0) (2026-06-06)


### Features

* **cli:** add headless reports, statusline, and long-context cost accuracy ([e83c373](https://github.com/janekbaraniewski/openusage/commit/e83c37397d54a1e41b61c39b2b1203ddb5a1cf10))
* **report:** add a loading spinner; fix(copilot): tolerate rotated session logs ([be820fa](https://github.com/janekbaraniewski/openusage/commit/be820fa4ec17f44add0fafc5fa618fc17c8055c8))
* **report:** add itemized usage for file-based providers (session/blocks) ([e7cdac2](https://github.com/janekbaraniewski/openusage/commit/e7cdac2c8458c2b536465e336d1632a60a22c442))
* **report:** extend session/blocks/daily to all telemetry-source providers ([b1c6071](https://github.com/janekbaraniewski/openusage/commit/b1c60717b1d1dcf583d6abc4c7ad484d96bd5cea))
* **tmux:** active-tool detection, install/uninstall, doctor, preview, watch, json ([6293d9c](https://github.com/janekbaraniewski/openusage/commit/6293d9cfede67d2ea6bb204402f6fd3b1b830fe4))
* **tmux:** formatter, presets, config, ccevents extraction ([f30f00b](https://github.com/janekbaraniewski/openusage/commit/f30f00bbba299b2d47ce53083c6299ddfb868aae))


### Dependencies

* **docs:** bump the docs-minor-and-patch group in /docs/site with 5 updates ([#179](https://github.com/janekbaraniewski/openusage/issues/179)) ([fd6aaf3](https://github.com/janekbaraniewski/openusage/commit/fd6aaf363821de99379637fee902e10710c9f8f2))
* **website:** bump the website-minor-and-patch group in /website with 4 updates ([#178](https://github.com/janekbaraniewski/openusage/issues/178)) ([9f8f068](https://github.com/janekbaraniewski/openusage/commit/9f8f0683f490caa9fccbad799463aa005cc556d9))

## [0.14.0](https://github.com/janekbaraniewski/openusage/compare/v0.13.0...v0.14.0) (2026-06-01)


### Features

* **telemetry:** derive true windowed credit spend from observed balance series ([b3a0fc5](https://github.com/janekbaraniewski/openusage/commit/b3a0fc5ea01a3c86afed561525216779dfc40b9e))


### Refactoring

* **credits:** show one windowed-spend figure for the active window ([4357caf](https://github.com/janekbaraniewski/openusage/commit/4357cafdd82190d480ff5dc86474d17651759916))

## [0.13.0](https://github.com/janekbaraniewski/openusage/compare/v0.12.0...v0.13.0) (2026-05-28)


### Features

* **cli:** add hub-view command with Bearer token auth and status-aware TUI ([930cc3c](https://github.com/janekbaraniewski/openusage/commit/930cc3c14d452c3231e85899a207d68002e1b8a7))
* **hub,exporter:** add hub server, exporter, and RemoteEnvelope for multi-machine aggregation ([8ce5e48](https://github.com/janekbaraniewski/openusage/commit/8ce5e48d562c6a50c75a42cada0ceb162a5e2417))
* **hub:** add /v1/snapshots endpoint, headless mode, daemon push integration, and Dockerfile ([1e3620a](https://github.com/janekbaraniewski/openusage/commit/1e3620acc9657cdeba479bbb46594c43ab55a2f8))


### Bug Fixes

* **hub:** address PR [#139](https://github.com/janekbaraniewski/openusage/issues/139) review comments + --allow-public guard ([93d90cb](https://github.com/janekbaraniewski/openusage/commit/93d90cbf5940accb42a215eeacc6b060fb4e2891))
* **hub:** printable snapshot keys, constant-time auth, 256 MiB fetch cap ([3986160](https://github.com/janekbaraniewski/openusage/commit/39861607013ea345f4456cc50e0dd7dcd1f9fb88))

## [0.12.0](https://github.com/janekbaraniewski/openusage/compare/v0.11.1...v0.12.0) (2026-05-28)


### Features

* amp provider ([ec92735](https://github.com/janekbaraniewski/openusage/commit/ec92735b83c1ad1c37e91a937fbd925627c7cdb7))
* claude code agent attribution ([1457e7f](https://github.com/janekbaraniewski/openusage/commit/1457e7fc3dc873949ec7aa42bc362e7875a424c3))
* codebuff provider ([af52e2d](https://github.com/janekbaraniewski/openusage/commit/af52e2d1fc39c3d13edf0c18c1b76829038f04c5))
* codex cost estimation ([67e0541](https://github.com/janekbaraniewski/openusage/commit/67e0541b3b5cd5b1ad4cd370bb95e5511dd3745a))
* codex model id resolution ([313fe6a](https://github.com/janekbaraniewski/openusage/commit/313fe6a789c1bb500704782b7aa226123ec006b2))
* copilot otel record dedup ([261d320](https://github.com/janekbaraniewski/openusage/commit/261d3208f87d6dce71a08fbd4fac04aef32e56a9))
* crush provider ([e5d9604](https://github.com/janekbaraniewski/openusage/commit/e5d9604d5387f5cd05fb546ba23b403426bd0eef))
* cursor csv export parser ([d9c15a2](https://github.com/janekbaraniewski/openusage/commit/d9c15a21459d62d69b6b239deb2cb8e71f76c274))
* droid provider ([629eaa7](https://github.com/janekbaraniewski/openusage/commit/629eaa71c4eeb0926da3f2fece291ee59dd1f260))
* gemini cli cost estimation ([e79fbc1](https://github.com/janekbaraniewski/openusage/commit/e79fbc11060bc5183f4db5746360c1219c5fcc64))
* gemini cli layout variants ([26fd594](https://github.com/janekbaraniewski/openusage/commit/26fd59435612a2abc23b5e3d131c2b03afdad3f6))
* goose provider ([9b94dcb](https://github.com/janekbaraniewski/openusage/commit/9b94dcbd7151715dab3b9949c5097048aea4ba70))
* hermes provider ([657dc49](https://github.com/janekbaraniewski/openusage/commit/657dc497253ef58873e8f419244b96d19c1ad1ee))
* json export command ([c5a599b](https://github.com/janekbaraniewski/openusage/commit/c5a599b37430057f37948517de8cac4ca1c30773))
* kilo provider ([98af01e](https://github.com/janekbaraniewski/openusage/commit/98af01e47e4c683cbf4d019aef7c00413e5fdd0d))
* kimi cli provider ([6f1d233](https://github.com/janekbaraniewski/openusage/commit/6f1d233dbc33c8529fc62c99ea04e638a0e0672c))
* mux provider ([0cd7d75](https://github.com/janekbaraniewski/openusage/commit/0cd7d752631fbb794c87bcc6582f87c72761eabd))
* openclaw provider ([860b23d](https://github.com/janekbaraniewski/openusage/commit/860b23d22562a76f08ddab423755f9bdb2cf40b1))
* opencode legacy and multi channel ([e04cbbf](https://github.com/janekbaraniewski/openusage/commit/e04cbbf7aa4dd2d822478a937edc5f48a0843377))
* pi provider ([dc3b01b](https://github.com/janekbaraniewski/openusage/commit/dc3b01b0d9c6ea990ad26a7f7a1c4a718f498d47))
* pricing pipeline ([c28f27e](https://github.com/janekbaraniewski/openusage/commit/c28f27e823ed923006ab2dcb419455986ee524c9))
* **pricing:** custom overrides and provider-preference ranking ([617f205](https://github.com/janekbaraniewski/openusage/commit/617f2051cbf3a760f39724ca0e52b6f37c9d41e8))
* qwen cli provider ([6253a10](https://github.com/janekbaraniewski/openusage/commit/6253a1066cb71e7dfef92c548a997807b18d9618))
* register pi, qwen_cli, openclaw, codebuff, kimi_cli ([5b3a24a](https://github.com/janekbaraniewski/openusage/commit/5b3a24af09d10be63fbfea8f23c2557d40a3eb20))
* roo code provider ([881a4a2](https://github.com/janekbaraniewski/openusage/commit/881a4a2247f26935b3002d19b6fa2c97ca34052a))
* zed provider ([bc550a9](https://github.com/janekbaraniewski/openusage/commit/bc550a994965d363f68e76da2de22879d6f46c06))


### Bug Fixes

* **#90:** detect opencode-go credentials in auth.json ([#162](https://github.com/janekbaraniewski/openusage/issues/162)) ([528559d](https://github.com/janekbaraniewski/openusage/commit/528559d0a80dea43c91c118cc1b01fe77cd9c375))
* **copilot,gemini_cli:** "today" metrics no longer mislabel last-active day ([45cc58e](https://github.com/janekbaraniewski/openusage/commit/45cc58e7fe9c6716ae9c6015f1979217dc281dd5))
* **crush:** replace speculative filesystem walk with project registry ([5a18c61](https://github.com/janekbaraniewski/openusage/commit/5a18c614763116c6271be2d0c966694fba1a77b8))
* **crush:** stop walking macOS-protected directories on first launch ([e725348](https://github.com/janekbaraniewski/openusage/commit/e725348117b44d27faacfbbfa293b9712c8665e5))
* path & session-id corrections across pi, openclaw, kimi_cli, kiro ([f81b629](https://github.com/janekbaraniewski/openusage/commit/f81b62953f6431def5777b3b2b50e691259f1730))
* surface parse-error & skipped-row diagnostics; clean crush Raw ([19850f9](https://github.com/janekbaraniewski/openusage/commit/19850f9f93a7d9b08ea49e1d00341bf60904801e))
* VS Code Server detection, dedup variant list, codebuff prefix ([92944f6](https://github.com/janekbaraniewski/openusage/commit/92944f648d64af7f3fc2e52c4d0711cbbae298f7))


### Performance

* **pricing:** cache normalized-key index and memoize Lookup results ([931b465](https://github.com/janekbaraniewski/openusage/commit/931b465c4dbf271c4273c901f76d0f600e8d6215))


### Dependencies

* **deps:** bump golang.org/x/crypto from 0.51.0 to 0.52.0 in the go-minor-and-patch group ([#166](https://github.com/janekbaraniewski/openusage/issues/166)) ([30d0246](https://github.com/janekbaraniewski/openusage/commit/30d02464a8311dbe69fa545ea1073ed818d64d8b))
* **docs:** bump protobufjs from 7.5.7 to 7.6.1 in /docs/site ([#171](https://github.com/janekbaraniewski/openusage/issues/171)) ([b643418](https://github.com/janekbaraniewski/openusage/commit/b6434184b18927ce0227d8bd6ae203e154b4e060))
* **docs:** bump qs and express in /docs/site ([#170](https://github.com/janekbaraniewski/openusage/issues/170)) ([934bb4e](https://github.com/janekbaraniewski/openusage/commit/934bb4e3055fd20a3fc831e628d48f52b7d95758))
* **docs:** bump the docs-minor-and-patch group across 1 directory with 2 updates ([#169](https://github.com/janekbaraniewski/openusage/issues/169)) ([0713928](https://github.com/janekbaraniewski/openusage/commit/07139286a8a61cff3c031afbbcf67f4771f21447))
* **website:** bump the website-minor-and-patch group across 1 directory with 4 updates ([#173](https://github.com/janekbaraniewski/openusage/issues/173)) ([5b7b7d2](https://github.com/janekbaraniewski/openusage/commit/5b7b7d274c5df19f0dd15e5965b20a99aad4338b))


### Refactoring

* claude_code dynamic pricing ([75ad528](https://github.com/janekbaraniewski/openusage/commit/75ad528daf8019654ac199f7c41287c028646e93))
* dedup crush walker, rename hermes CacheWriteTok ([c71ea19](https://github.com/janekbaraniewski/openusage/commit/c71ea192c6241017a732ff9ebd8e54cb81b8f36f))

## [0.11.1](https://github.com/janekbaraniewski/openusage/compare/v0.11.0...v0.11.1) (2026-05-18)


### Dependencies

* **docs:** bump ws 8.20.0 → 8.20.1 (CVE-2026-45736) ([1f06c00](https://github.com/janekbaraniewski/openusage/commit/1f06c00d3e50024c807e4ebd8d76526dd7ebf407))

## [0.11.0](https://github.com/janekbaraniewski/openusage/compare/v0.10.6...v0.11.0) (2026-05-18)


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
