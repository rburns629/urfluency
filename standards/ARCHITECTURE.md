# Architecture

Preserve intentional architecture and domain semantics. Choose complexity from
current requirements, not the popularity of Clean Architecture, hexagonal design,
domain-driven design, or microservices.

## Design boundaries

Separate concerns into cohesive modules with explicit ownership and domain boundaries.
Keep dependencies directed toward stable contracts; avoid circular dependencies.
Encapsulate invariants and mutable state. Expose small interfaces with explicit
inputs, outputs, errors, and side effects. Keep coupling visible and controlled.
Use straightforward functional decomposition when it makes responsibilities clearer.
Prefer composition when behavior must vary independently; inheritance is appropriate
for a genuine substitutable relationship with a stable behavioral contract.

When the same logic serves several real data sources or providers, such as a live
database and a cache, keep that logic in one place and put the variation behind a
narrow boundary. Avoid both duplicating functions per source and branching on the
source inside every function. Keep independent mechanisms separate: pagination,
external batch limits, and concurrency are simpler as distinct steps than as one
combined loop.

Protect public contracts, including error behavior and serialization. Refactor in
small verifiable steps; preserve compatibility or document an approved migration.
Follow existing repository patterns unless evidence shows a concrete defect.

## SOLID as decision guidance

Single responsibility means cohesive reasons to change, not one method per class.
Open/closed design can stabilize a proven variation point without predicting every
future extension. Substitution includes invariants and error behavior, not merely
matching signatures. Interface segregation serves actual consumers. Dependency
inversion is useful when it reduces meaningful coupling or isolates external effects.
None requires an interface per class, a factory per dependency, or a dependency
injection container where direct construction suffices.

Before adding an abstraction, answer in review:

1. What problem does it solve?
2. Does the requirement exist today?
3. Could a simpler implementation satisfy it?
4. Does it reduce meaningful coupling?
5. Does it improve testability or ownership?
6. Does it align with this repository?
7. What maintenance burden does it introduce?

A payment-provider boundary justified by two supported providers and contract tests
can isolate meaningful variation. A plugin registry, factory, and interface around
one pure calculation with no variation requirement adds unjustified machinery.

## Evidence

Review dependency direction, owners, representative call paths, contract tests, and
the rationale for new layers. Use dependency-cycle tooling where available; tooling
cannot decide whether a domain boundary or abstraction is useful.
