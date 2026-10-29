# tree-sitter-swift (vendored)

The Swift grammar's Go module does not include its generated parser, so it cannot
be used as a dependency. These files are copied from the npm package
`tree-sitter-swift@0.7.1` (MIT, github.com/alex-pinkus/tree-sitter-swift):
`parser.c`, `scanner.c`, `tree_sitter/*.h` (from `src/` in the package), and `LICENSE`.

- npm integrity: `sha512-pneKVTuGamaBsqqqfB9BvNQjktzh/0IVPR54jLB5Fq/JTDQwYHd0Wo6pVyZ5jAYpbztzq+rJ/rpL9ruxTmSoKw==`
- Parser ABI: 14
- `scanner.c` includes only `tree_sitter/parser.h`, `stdio.h`, `stdlib.h`, `string.h`, and `wctype.h`.

`scanner.c` carries three upstream memory-safety fixes made after 0.7.1, which
has no later release; AddressSanitizer found the first in our nightly fuzz job:

- 8abb3e8b33 (2026-03-20): the scanner state was allocated with
  `calloc(0, ...)`, so every Swift parse read and wrote past it.
- 35245fbfee (2026-09-28): exit cleanly if that allocation fails.
- 0c5444e4f1 (2026-10-10): a compiler directive at the end of the input read
  past the end of the directive name.

Drop these notes when updating to a release that includes them.

To update: `npm pack tree-sitter-swift@<version>` in an empty directory, check the
integrity against `npm view`, review `scanner.c`, copy the same files, update this
README and `binding.go`, and run the Swift analyzer tests.
