# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

## [1.0.0] - 2023-12-28

Bumping to proper release version.

### Changed

- Truncate downloaded file if checksums mismatch. This allows graceful recovery on a retry
  instead of having to restart the entire process.

## [0.0.1] - 2023-12-27

### Added

- A changelog.

### Changed

- Better release infrastructure on the road to making releases with GitHub actions.
