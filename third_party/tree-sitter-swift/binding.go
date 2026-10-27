// Package swift is the tree-sitter Swift grammar (alex-pinkus/tree-sitter-swift
// 0.7.1, MIT), vendored because its Go module does not ship the generated parser.
// cgo compiles parser.c and scanner.c as separate units. See README.md for
// provenance and how to update it.
package swift

// #cgo CFLAGS: -std=c11 -fPIC
// typedef struct TSLanguage TSLanguage;
// const TSLanguage *tree_sitter_swift(void);
import "C"

import "unsafe"

// Language returns the tree-sitter language for Swift.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_swift())
}
