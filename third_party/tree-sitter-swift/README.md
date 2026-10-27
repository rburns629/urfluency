# tree-sitter-swift (vendored)

The Swift grammar's Go module does not include its generated parser, so it cannot
be used as a dependency. These files are copied unchanged from the npm package
`tree-sitter-swift@0.7.1` (MIT, github.com/alex-pinkus/tree-sitter-swift):
`parser.c`, `scanner.c`, `tree_sitter/*.h` (from `src/` in the package), and `LICENSE`.

- npm integrity: `sha512-pneKVTuGamaBsqqqfB9BvNQjktzh/0IVPR54jLB5Fq/JTDQwYHd0Wo6pVyZ5jAYpbztzq+rJ/rpL9ruxTmSoKw==`
- Parser ABI: 14
- `scanner.c` includes only `tree_sitter/parser.h`, `string.h`, and `wctype.h`.

To update: `npm pack tree-sitter-swift@<version>` in an empty directory, check the
integrity against `npm view`, review `scanner.c`, copy the same files, update this
README and `binding.go`, and run the Swift analyzer tests.
