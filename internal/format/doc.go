// Package format holds parsing helpers shared by more than one source
// implementation (ResolveColumns, ResolveDateFormat, DefaultFormatForKind).
// The implementations themselves -- csv, jsonl, xlsx, console -- are its
// subpackages, each registering itself with internal/runtime's format
// registry from its own init(), per CLAUDE.md's format-registry
// convention: adding a format is one subpackage + one registry entry,
// with nothing here or above it special-cased on a format's name.
package format
