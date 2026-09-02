package tool

import "fmt"

// BuiltinDefinitions returns the application tool facts in canonical catalog
// order. The returned slice and its argument slices belong to the caller.
func BuiltinDefinitions() []Definition {
	return []Definition{
		{
			ID: "sysbench", PurposeKey: "doctor.purpose.sysbench",
			Verification: VerificationPolicy{Kind: VerificationCommand, Arguments: []string{"--version"}},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 0},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "zstd", PurposeKey: "doctor.purpose.zstd",
			Verification: VerificationPolicy{Kind: VerificationPinnedZstd, Arguments: []string{"--version"}, ExpectedVersion: "1.5.7", SuccessLabel: "zstd 1.5.7"},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 1},
			Staging:      StagingPolicy{Category: StagingZstdCorpus},
		},
		{
			ID: "npb-ep", PurposeKey: "doctor.purpose.npbEP",
			Verification: VerificationPolicy{Kind: VerificationNPB, ExpectedVersion: "3.4.4", NPBVariant: NPBVariantEP, SuccessLabel: "NPB 3.4.4 EP (Class A verified at run)"},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 2},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "npb-ft", PurposeKey: "doctor.purpose.npbFT",
			Verification: VerificationPolicy{Kind: VerificationNPB, ExpectedVersion: "3.4.4", NPBVariant: NPBVariantFT, SuccessLabel: "NPB 3.4.4 FT (Class A verified at run)"},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 3},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "openssl", PurposeKey: "doctor.purpose.openssl",
			Verification: VerificationPolicy{Kind: VerificationPinnedOpenSSL, Arguments: []string{"version"}, ExpectedVersion: "3.5.7", SuccessLabel: "OpenSSL 3.5.7"},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 4},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "stream", PurposeKey: "doctor.purpose.stream",
			Verification: VerificationPolicy{Kind: VerificationOfficialStream, SuccessLabel: "official STREAM"},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 7},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "fio", PurposeKey: "doctor.purpose.fio",
			Verification: VerificationPolicy{Kind: VerificationCommand, Arguments: []string{"--version"}},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 5},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "iperf3", PurposeKey: "doctor.purpose.iperf3",
			Verification: VerificationPolicy{Kind: VerificationCommand, Arguments: []string{"--version"}},
			Doctor:       DoctorPolicy{Standard: true, Required: true, Order: 6},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "nexttrace-tiny", PurposeKey: "doctor.purpose.nexttrace",
			Verification: VerificationPolicy{Kind: VerificationCommand, Arguments: []string{"--version"}},
			Doctor:       DoctorPolicy{Standard: true, Required: false, Order: 8},
			Staging:      StagingPolicy{Category: StagingNextTrace, Source: StagingSourceNextTraceArchitecture},
		},
		{
			ID: "ping", PurposeKey: "doctor.purpose.ping",
			Verification: VerificationPolicy{Kind: VerificationCommand, Arguments: []string{"-V"}},
			Doctor:       DoctorPolicy{Standard: true, Required: false, Order: 9},
			Staging:      StagingPolicy{Category: StagingArchive},
		},
		{
			ID: "speedtest", PurposeKey: "doctor.purpose.speedtest",
			Verification: VerificationPolicy{Kind: VerificationCommand, Arguments: []string{"--version"}},
			Doctor:       DoctorPolicy{Standard: true, Required: false, Order: 10},
			Staging:      StagingPolicy{Category: StagingOokla, Source: StagingSourceOoklaSignedPackage},
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
