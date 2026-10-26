# Type safety and data contracts

Model domain distinctions accurately: identifiers, units, states, and invariants.
Use explicit types where they clarify contracts. Distinguish absent, null, empty,
and invalid values. Represent expected failures through the language's established
error/result conventions; do not hide errors behind misleading default values.

Use generics for real shared relationships, with constraints that preserve meaning.
Choose structural typing for compatible shapes and nominal types when accidental
interchange would violate domain semantics. Keep shared contracts consistent across
modules without creating an unnecessary global model layer.

Validate external data at trust boundaries before treating it as trusted. Static
types do not validate network messages, configuration, database rows, or serialized
data. Define serialization formats, unknown-field behavior, ranges, and compatibility.
Evolve schemas with explicit versioning or compatible transitions and migration tests.
Use safe narrowing and exhaustive state handling where supported. Avoid unchecked
casts and assertions; isolate unavoidable foreign-interface casts with validation,
rationale, and tests. Do not suppress analyzers broadly to satisfy a gate.

## Language interpretation

- TypeScript: use strict compiler checks and accurate unions; contain `unknown` at
  boundaries and avoid widespread `any` or assertions that bypass validation.
- Python: use meaningful annotations and appropriate static analysis; avoid repeated
  runtime type checks inside trusted code when boundary validation is sufficient.
- Go: use small consumer-scoped interfaces when useful and explicit error handling;
  do not proliferate interfaces around every implementation.
- Java: use coherent domain types, generics, explicit nullability conventions, and
  established exception handling; avoid raw types and unchecked conversions.
- Rust: preserve ownership and lifetime safety; justify cloning and isolate any
  necessary `unsafe` behind documented invariants and focused verification.
- Other languages: apply native contract and analysis mechanisms; dynamic languages
  can use schemas, annotations, boundary checks, and behavioral tests as appropriate.

## Evidence

Run the repository's applicable compiler/type checker/static analyzer. Review
boundary validation, casts, null handling, and schema compatibility. Explain any
unsupported checks and narrow exceptions using [quality gates](QUALITY_GATES.md).
