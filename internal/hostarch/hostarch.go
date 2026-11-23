// SPDX-License-Identifier: GPL-3.0-only

// Package hostarch describes the CPU architecture of the host. netrave starts virtual machines
// only with KVM, and KVM runs guests of the host's own architecture, so the QEMU target, the
// emulator binary and the machine type all follow from the host.
package hostarch

import "runtime"

// QEMUArch returns the QEMU target name of the host: "x86_64" or "aarch64".
func QEMUArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

// QEMUBinary returns the name of the QEMU system emulator for the host.
func QEMUBinary() string {
	return "qemu-system-" + QEMUArch()
}

// DefaultMachine returns the machine type of the standard template for the host:
// "pc" on x86_64, "virt" on aarch64.
func DefaultMachine() string {
	if QEMUArch() == "aarch64" {
		return "virt"
	}
	return "pc"
}

// DefaultBIOS returns the firmware of the standard template: SeaBIOS on x86_64; aarch64 has no
// SeaBIOS and always boots through UEFI.
func DefaultBIOS() string {
	if QEMUArch() == "aarch64" {
		return "uefi"
	}
	return "seabios"
}
