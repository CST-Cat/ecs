package tool

// BuiltinDefinitions returns the application tool facts in canonical order.
// The returned slice and its values belong to the caller.
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

// LookupBuiltin returns one built-in tool fact by exact ID.
func LookupBuiltin(id string) (Definition, bool) {
	for _, definition := range BuiltinDefinitions() {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}
