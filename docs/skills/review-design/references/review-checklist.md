# Design Doc Review Checklist

Check each category against the actual codebase. Only flag concrete mismatches.

## Types & Interfaces

- [ ] All types referenced in the design exist in the codebase (or are clearly marked as "new")
- [ ] Field names, types, and JSON tags match current definitions
- [ ] Interface methods match current signatures (receiver, params, return types)
- [ ] Embedded structs still exist and have the expected fields
- [ ] Enum/const values referenced in the design match current definitions

## Files & Packages

- [ ] Every file path in implementation tasks exists (or is marked as "create")
- [ ] Package names match the directory structure
- [ ] Import paths are correct for the module (`github.com/openusage/openusage/...` or current module path)

## Function Signatures

- [ ] Functions the design calls actually exist with matching signatures
- [ ] Receiver types are correct (pointer vs value)
- [ ] Return types haven't changed since the design was written
- [ ] Helper functions referenced (e.g., from `shared/helpers.go`) still exist

## Config Schema

- [ ] New config fields match the existing `Config` struct pattern
- [ ] JSON field names follow existing conventions (`snake_case`, `omitempty` for optional)
- [ ] Default values are consistent with `defaultConfig()` patterns
- [ ] `example_settings.json` changes are compatible

## Provider Contract

- [ ] `UsageProvider` interface methods haven't changed
- [ ] `ProviderSpec` / `DashboardWidget` / `DetailWidget` fields are current
- [ ] Provider registration pattern in `registry.go` is current
- [ ] `AccountConfig` fields used by the design still exist and behave as expected

## TUI Integration

- [ ] Message types referenced in the design exist in `tui/`
- [ ] Key bindings don't conflict with existing bindings
- [ ] View functions referenced are current
- [ ] Widget rendering patterns match current approach

## Telemetry & Daemon

- [ ] Event types and store methods are current
- [ ] Read model aggregation functions exist
- [ ] Socket/IPC protocol hasn't changed
- [ ] Pipeline stages referenced are current

## Dependencies

- [ ] Task dependency order is valid (no circular deps, no missing prerequisites)
- [ ] External packages referenced are in `go.mod`
- [ ] No tasks depend on types/functions from later tasks
