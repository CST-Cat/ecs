package tool

import "fmt"

// BuiltinDefinitions returns the application tool facts in canonical catalog
// order. The returned slice and its values belong to the caller.
func BuiltinDefinitions() []Definition {
	return []Definition{
		{
			ID: "sysbench",
		},
		{
			ID: "zstd",
		},
		{
			ID: "npb-ep",
		},
		{
			ID: "npb-ft",
		},
		{
			ID: "openssl",
		},
		{
			ID: "stream",
		},
		{
			ID: "fio",
		},
		{
			ID: "iperf3",
		},
		{
			ID: "nexttrace-tiny",
		},
		{
			ID: "ping",
		},
		{
			ID: "speedtest", ExternalService: "ookla",
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
