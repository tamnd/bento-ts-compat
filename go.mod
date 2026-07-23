module github.com/tamnd/bento-ts-compat

go 1.26.5

// bento's ahead-of-time front-end reaches typescript-go internals through an
// additive fork. We pin the same fork revision bento pins, so the checker that
// accepts or rejects a case here is the exact one bento ships.
replace github.com/microsoft/typescript-go => github.com/tamnd/typescript v0.0.0-20260722183216-adb2ba1e4627

require github.com/tamnd/bento v0.0.0-20260723090452-cd5a31c09c73

require (
	github.com/evanw/esbuild v0.28.1 // indirect
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/microsoft/typescript-go v0.0.0-00010101000000-000000000000 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
)
