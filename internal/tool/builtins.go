package tool

import "fmt"

// BuiltinDefinitions returns the application tool facts in canonical catalog
// order. The returned slice and its values belong to the caller.
func BuiltinDefinitions() []Definition {
	return []Definition{
		{
			ID: "sysbench", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "zstd", Staging: StagingPolicy{Category: StagingZstdCorpus},
		},
		{
			ID: "npb-ep", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "npb-ft", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "openssl", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "stream", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "fio", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "iperf3", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "nexttrace-tiny", Staging: StagingPolicy{Category: StagingNextTrace, Source: StagingSourceNextTraceArchitecture},
		},
		{
			ID: "ping", Staging: StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "speedtest", Staging: StagingPolicy{Category: StagingOokla, Source: StagingSourceOoklaSignedPackage},
		},
	}
}

// BuiltinCatalog validates a fresh copy of the built-in tool facts on every
// call. It retains no mutable global registry and returns an explicit error if
// a source edit violates the contract.
func BuiltinCatalog() (Catalog, error) {
	catalog, err := NewCatalog(BuiltinDefinitions())
	if err != nil {
		return Catalog{}, fmt.Errorf("builtin tool catalog: %w", err)
	}
	return catalog, nil
}
