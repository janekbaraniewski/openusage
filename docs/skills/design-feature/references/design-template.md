# <Feature Name> Design

Date: <YYYY-MM-DD>
Status: Proposed
Author: <author>

## 1. Problem Statement

<One sentence: what's broken or missing today.>

## 2. Goals

1. <Goal 1>
2. <Goal 2>

## 3. Non-Goals

1. <Explicitly out of scope item 1>
2. <Explicitly out of scope item 2>

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | <none/minor/major> | <one-line description> |
| providers | <none/minor/major> | <which providers, what changes> |
| TUI | <none/minor/major> | <which screens/components> |
| config | <none/minor/major> | <new fields, schema changes> |
| detect | <none/minor/major> | <new detection logic> |
| daemon | <none/minor/major> | <collection/caching changes> |
| telemetry | <none/minor/major> | <pipeline/store changes> |
| CLI | <none/minor/major> | <new commands or flags> |

### Existing Design Doc Overlap

<Reference any existing docs in docs/ that relate. State whether this design extends or supersedes them.>

## 5. Detailed Design

### 5.1 <Component/Change Area 1>

<Describe the change. Include Go type definitions if adding/modifying types. Show the minimal code that communicates the design — not full implementations.>

### 5.2 <Component/Change Area 2>

<Continue for each distinct area of change.>

### 5.N Backward Compatibility

<How existing configs, data, and behavior are preserved. Migration steps if needed.>

## 6. Alternatives Considered

### <Alternative 1>

<What it is, why it was rejected.>

## 7. Implementation Tasks

### Task 1: <title>
Files: <files to create or modify>
Depends on: none
Description: <what to do>
Tests: <what tests to write>

### Task 2: <title>
Files: <files to create or modify>
Depends on: Task 1
Description: <what to do>
Tests: <what tests to write>

### Dependency Graph

<Summarize which tasks can run in parallel vs. must be sequential.>

```
Sequential: Task 1 → Task 2
Parallel group: Tasks 3, 4, 5 (all depend on 1-2)
Sequential: Task 6 (depends on 3, 4) → Task 7 (depends on all)
```
