// Defaults of a machine template for a host architecture: the machine type and the firmware
// that the emulator of that architecture supports. Keep in step with internal/hostarch.
export function templateDefaults(arch: string): { machine: string; bios: 'seabios' | 'uefi' } {
  return arch === 'aarch64' ? { machine: 'virt', bios: 'uefi' } : { machine: 'pc', bios: 'seabios' };
}
