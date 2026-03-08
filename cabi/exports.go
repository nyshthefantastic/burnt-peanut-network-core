package cabi

/*
every function in the core.h file must be exported to be called from C.
for that we need a go implementation for each function in the core.h file.


we need to use //export comment so that CGo makes them visible to C.

- The `//export` comment must be **directly above** the function, no blank line
- Parameters and return types must be **C types** (`C.int32_t`, `C.uintptr_t`, `*C.uint8_t`)
- We must convert between C and Go types manually

*/

