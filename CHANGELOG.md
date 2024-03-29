<!-- markdownlint-disable MD013 -->
<!-- markdownlint-disable MD024 -->

<!--
Changelog Guiding Principles:

Changelogs are for humans, not machines.
There should be an entry for every single version.
The same types of changes should be grouped.
Versions and sections should be linkable.
The latest version comes first.
The release date of each version is displayed.
Mention whether you follow Semantic Versioning.

Usage:

Change log entries are to be added to the Unreleased section under the
appropriate stanza (see below). Each entry should ideally include a tag and
the Github PR referenced in the following format:

* (<tag>) [#<PR-number>](https://github.com/ojo-network/price-feeder/pull/<PR-number>) <changelog entry>

Types of changes (Stanzas):

State Machine Breaking: for any changes that result in a divergent application state.
Features: for new features.
Improvements: for changes in existing functionality.
Deprecated: for soon-to-be removed features.
Bug Fixes: for any bug fixes.
Client Breaking: for client breaking changes.
API Breaking: for breaking exported Go APIs used by developers.
Config: updates to the recommended configuration files

To release a new version, ensure an appropriate release branch exists. Add a
release version and date to the existing Unreleased section which takes the form
of:

## [<version>](https://github.com/ojo-network/price-feeder/releases/tag/<version>) - YYYY-MM-DD

Once the version is tagged and released, a PR should be made against the main
branch to incorporate the new changelog updates.

Ref: https://keepachangelog.com/en/1.0.0/
-->

# Changelog

## [Unreleased]

## [v0.1.5](https://github.com/ojo-network/price-feeder/releases/tag/v0.1.6-rc1) - 2023-08-10


### Features

- [210](https://github.com/ojo-network/price-feeder/pull/210) Error handling updates

### Improvements
- [220](https://github.com/ojo-network/price-feeder/pull/220) Change OsmosisV2 provider to Osmosis

### Config

- [215](https://github.com/ojo-network/price-feeder/pull/215) Add rETH to sample config

## [v0.1.5](https://github.com/ojo-network/price-feeder/releases/tag/v0.1.5) - 2023-08-10

### Features

- [192](https://github.com/ojo-network/price-feeder/pull/192) Skip specific denoms in integration test.
- [157](https://github.com/ojo-network/price-feeder/pull/157) Add Kujira provider.
- [137](https://github.com/ojo-network/price-feeder/pull/137) Uniswap v3 integration.

### Config

- [183](https://github.com/ojo-network/price-feeder/pull/183) Add SCRT, JUNO, and stJUNO to sample config.
