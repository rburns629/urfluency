# Testing

Choose tests for behavioral confidence and change risk, not test count or arbitrary
100% coverage. Follow the project's tooling and conventions. Assert observable
contracts and meaningful outcomes rather than mirroring implementation structure.

## Select appropriate layers

- Unit tests: domain rules and focused success, boundary, and failure behavior.
- Integration tests: persistence, queues, services, and actual adapter behavior.
- Contract tests: producer/consumer compatibility, serialization, and errors.
- End-to-end tests: critical user journeys across deployed components.
- Regression tests: reproduce confirmed defects and verify fixes when practical.
- Security tests: validation, authorization denials, isolation, and abuse paths.
- Concurrency tests: ordering, races, retries, cancellation, and duplicate delivery.
- Property-based tests: meaningful invariants over broad input spaces.
- Performance tests: documented latency, throughput, and resource requirements.

Apply layers when the changed behavior needs them; do not require every category
for every change. Include unsuccessful behavior where it has meaningful consequences.

## Reliable execution

Keep tests isolated and deterministic: control clocks, randomness, ordering, and
external state. Use synthetic non-sensitive data, unique resources, and constrained
test credentials. Review setup and cleanup for filesystem and data safety; tests
are not authorization to delete existing files or access production systems.

Mock stable boundaries to focus a unit test. Verify real infrastructure semantics
when mocks cannot establish correctness, such as transaction isolation, migrations,
or protocol behavior. Never infer integration correctness solely from mocks.
Document infrastructure versions and prerequisites. Investigate flaky failures;
quarantine only with an owner, reason, and remediation plan, not silent exclusion.

Do not delete or weaken tests simply to pass a build. When requirements change,
justify and authorize updates to old expectations. Avoid meaningless coverage
assertions. Record exact commands, outcomes, and environment limitations. If tests
cannot run safely or infrastructure is unavailable, report Not run with the reason
and the remaining verification needed. Never claim unexecuted tests passed.
