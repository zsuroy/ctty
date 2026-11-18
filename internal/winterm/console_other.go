//go:build !windows

package winterm

// IsLegacyConsole is false off Windows.
func IsLegacyConsole() bool { return false }

// PrepareForTUI is a no-op off Windows.
func PrepareForTUI() func() { return func() {} }

// PrepareForTUIChild is a no-op off Windows.
func PrepareForTUIChild() func() { return func() {} }

// PrepareForCLIChild is a no-op off Windows.
func PrepareForCLIChild() func() { return func() {} }
