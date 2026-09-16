//go:build windows

package probe

import "ecs/internal/model"

// Windows does not expose the Unix kernel-tuning surface used by this table.
// Keep the table empty and the logical fields unavailable rather than
// attaching a Linux method ID to a guessed value.
func platformKernelParams() []kernelParam { return nil }

func appendKernelNetworkParams(_ *model.Result) {}

func kernelRmemMethod() string { return "" }

func platformKernelRmemMaxAvailable() bool { return false }
