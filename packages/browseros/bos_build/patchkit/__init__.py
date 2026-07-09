"""Python patch surface: dev extract + pipeline batch-apply + features.yaml IO.

Interactive patch workflows (apply/sync/conflicts) live in the Go tool
(tools/patch, `bpatch`); the build pipeline must never depend on it, so
non-interactive batch apply stays here in Python by design.
"""
