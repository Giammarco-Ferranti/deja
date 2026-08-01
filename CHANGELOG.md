# Changelog

## [0.4.0](https://github.com/Giammarco-Ferranti/deja/compare/v0.3.2...v0.4.0) (2026-08-01)


### Features

* Add option to suppress suggestions on an empty prompt ([f5db7de](https://github.com/Giammarco-Ferranti/deja/commit/f5db7de66951d781e5321f9dc8ceabc850d1c353))
* bind Shift+up to toggle empty-prompt suggestions ([8ca6935](https://github.com/Giammarco-Ferranti/deja/commit/8ca6935b5b6cc8530c158a955db649cd428245e4))


### Bug Fixes

* deja now correctly respects HIST_IGNORE_SPACE and HIST_IGNORE ([83a8ad5](https://github.com/Giammarco-Ferranti/deja/commit/83a8ad58a1b82bc2d0c0ca477ff82b2320451f93))
* keep ghost text styled and anchored after completion widgets ([54d3f55](https://github.com/Giammarco-Ferranti/deja/commit/54d3f55ddfb2e31cfd80c415c492cdf34c49d3e7))
* restrict data directory and database to the owning user ([#79](https://github.com/Giammarco-Ferranti/deja/issues/79)) ([95264e2](https://github.com/Giammarco-Ferranti/deja/commit/95264e2d0eb440fae07a1d23eba0e00ac26a9e5c))

## [0.3.2](https://github.com/Giammarco-Ferranti/deja/compare/v0.3.1...v0.3.2) (2026-06-11)


### Bug Fixes

* prevent shell breakage from completion widget wrapping and missing binary ([#65](https://github.com/Giammarco-Ferranti/deja/issues/65)) ([be80999](https://github.com/Giammarco-Ferranti/deja/commit/be80999e8b872c591db428c298eb02b4464a5536))

## [0.3.1](https://github.com/Giammarco-Ferranti/deja/compare/v0.3.0...v0.3.1) (2026-06-09)


### Bug Fixes

* multiline commands now correclty interpreted ([#63](https://github.com/Giammarco-Ferranti/deja/issues/63)) ([9e1f541](https://github.com/Giammarco-Ferranti/deja/commit/9e1f5412fc46c71ce63b93b01d7e57e05d7b2bc5))

## [0.3.0](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.7...v0.3.0) (2026-06-04)


### Features

* add fuzzy matching modes (smart/loose/tight) with cycle keybinding ([e6db73f](https://github.com/Giammarco-Ferranti/deja/commit/e6db73f5f94b0b74e0c8cfa4089cff10be33c447))
* configurable suggestion keybindings ([#54](https://github.com/Giammarco-Ferranti/deja/issues/54), [#52](https://github.com/Giammarco-Ferranti/deja/issues/52)) ([#60](https://github.com/Giammarco-Ferranti/deja/issues/60)) ([532ea39](https://github.com/Giammarco-Ferranti/deja/commit/532ea39090c0df2afc162091020c2f80c9bc8f99))
* implemented deja.plugin.zsh to ensure deja can be used as a cus… ([#61](https://github.com/Giammarco-Ferranti/deja/issues/61)) ([a35ba3c](https://github.com/Giammarco-Ferranti/deja/commit/a35ba3c138e0220618b77ea4da27cef3e4ef8807))

## [0.2.7](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.6...v0.2.7) (2026-05-26)


### Bug Fixes

* importer now correctly uses HISTFILE and fallback to .zsh_history. Users can use the new --file flag to specify a file explicitly ([#56](https://github.com/Giammarco-Ferranti/deja/issues/56)) ([7ee138f](https://github.com/Giammarco-Ferranti/deja/commit/7ee138f18aaf121d4fc3f8f96657a19817a43f86))

## [0.2.6](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.5...v0.2.6) (2026-05-22)


### Bug Fixes

* deja conflict with zsh-autosuggestion ([#48](https://github.com/Giammarco-Ferranti/deja/issues/48)) ([d425eea](https://github.com/Giammarco-Ferranti/deja/commit/d425eea13001f230d785b375e52f216f37d15cd8))

## [0.2.5](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.4...v0.2.5) (2026-05-15)


### Bug Fixes

* batch insert into store now works correctly.  ([#41](https://github.com/Giammarco-Ferranti/deja/issues/41)) ([016b981](https://github.com/Giammarco-Ferranti/deja/commit/016b98113b53e98281bc52e7ae5386ab5cba6b3c))

## [0.2.4](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.3...v0.2.4) (2026-05-11)


### Features

* Docs per deja release ([#34](https://github.com/Giammarco-Ferranti/deja/issues/34)) ([20330ba](https://github.com/Giammarco-Ferranti/deja/commit/20330bad0c54656f138bd15852f459b17231922c))

## [0.2.3](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.2...v0.2.3) (2026-05-10)


### Bug Fixes

* deamon recovers and test gaps in deja ([#30](https://github.com/Giammarco-Ferranti/deja/issues/30)) ([95c5b49](https://github.com/Giammarco-Ferranti/deja/commit/95c5b4972822fdb710df2f41d255d50562220f08))

## [0.2.2](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.1...v0.2.2) (2026-05-10)


### Bug Fixes

* **daemon:** refresh in-memory stats on Record so new commands surface immediately ([#22](https://github.com/Giammarco-Ferranti/deja/issues/22)) ([#23](https://github.com/Giammarco-Ferranti/deja/issues/23)) ([5fb69d7](https://github.com/Giammarco-Ferranti/deja/commit/5fb69d7d3043b9138a2fb2df539d3599127c919c))

## [0.2.1](https://github.com/Giammarco-Ferranti/deja/compare/v0.2.0...v0.2.1) (2026-05-05)


### Bug Fixes

* homebrew install and new install script ([#18](https://github.com/Giammarco-Ferranti/deja/issues/18)) ([#19](https://github.com/Giammarco-Ferranti/deja/issues/19)) ([5105232](https://github.com/Giammarco-Ferranti/deja/commit/510523200ceb511870b3cef10a68a0ac5429c29b))

## [0.2.0](https://github.com/Giammarco-Ferranti/deja/compare/v0.1.1...v0.2.0) (2026-05-05)


### Features

* added test.yml ci to ensure tests pass before releasing ([ebda2cd](https://github.com/Giammarco-Ferranti/deja/commit/ebda2cd837cfcdfa98903ca7b4248fd6f8297c63))
* added test.yml ci to ensure tests pass before releasing ([d8b9e52](https://github.com/Giammarco-Ferranti/deja/commit/d8b9e52b42bd373252f780f4fbe2869d946e00bf))
* all commands now give a clear explanation on what they do ([17c0eaa](https://github.com/Giammarco-Ferranti/deja/commit/17c0eaa3af6a8aefda4edfbf58575b4b2e39f693))
* all commands now give a clear explanation on what they do ([a01fe4a](https://github.com/Giammarco-Ferranti/deja/commit/a01fe4ab8fd3c7d52e4ab2b7bd98b512292595f0))
* created tagged release produce useful artifacts ([d495f62](https://github.com/Giammarco-Ferranti/deja/commit/d495f626e2d8ef07f59e6015b01c50510b4a6cc8))
* created tagged release produce useful artifacts ([cefa035](https://github.com/Giammarco-Ferranti/deja/commit/cefa035f627d5edfd05aa7ab0aa5c58266d9039c))


### Bug Fixes

* removed count to 100 and issue in zsh shell ([80e2fd2](https://github.com/Giammarco-Ferranti/deja/commit/80e2fd2d8c422c649d96c1f8d1025e25b348f36a))
