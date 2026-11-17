import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  importLab: vi.fn(),
  placeLab: vi.fn(),
  toast: { success: vi.fn(), warning: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock('../../api/endpoints', () => ({
  labsApi: { importLab: mocks.importLab },
  foldersApi: { placeLab: mocks.placeLab },
}));
vi.mock('../../stores/toast', () => ({ toast: mocks.toast }));

import { IMPORT_ACCEPT, MAX_IMPORT_BYTES, importLabFile } from './importLab';

// jsdom does not implement Blob.text(); browsers do, so add it to the file here.
const file = (content: string, name = 'lab.yaml') => {
  const f = new File([content], name, { type: 'application/yaml' });
  Object.defineProperty(f, 'text', { value: async () => content });
  return f;
};
const exportYAML = 'format: netrave-lab\nversion: 1\nname: Test\ndevices: []\nnetworks: []\nconnections: []\n';

describe('importLabFile', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sends the file text as is: the server parses YAML', async () => {
    mocks.importLab.mockResolvedValue({ id: 42, warnings: [] });
    mocks.placeLab.mockResolvedValue({ ok: true });

    const id = await importLabFile(file(exportYAML), 7);

    expect(id).toBe(42);
    expect(mocks.importLab).toHaveBeenCalledWith(exportYAML);
    expect(mocks.placeLab).toHaveBeenCalledWith(42, 7);
    expect(mocks.toast.success).toHaveBeenCalled();
  });

  it('keeps the lab at the root when no folder is open', async () => {
    mocks.importLab.mockResolvedValue({ id: 5 });
    await importLabFile(file(exportYAML), 0);
    expect(mocks.placeLab).not.toHaveBeenCalled();
  });

  it('does not call the server for files larger than the limit', async () => {
    const big = file('x');
    Object.defineProperty(big, 'size', { value: MAX_IMPORT_BYTES + 1 });
    await expect(importLabFile(big, 0)).rejects.toThrow();
    expect(mocks.importLab).not.toHaveBeenCalled();
  });

  it('reports server warnings without failing the import', async () => {
    mocks.importLab.mockResolvedValue({ id: 9, warnings: ['образ не найден'] });
    const id = await importLabFile(file(exportYAML), 0);
    expect(id).toBe(9);
    expect(mocks.toast.warning).toHaveBeenCalled();
  });

  it('offers only YAML in the file picker', () => {
    expect(IMPORT_ACCEPT).toContain('.yaml');
    expect(IMPORT_ACCEPT).not.toContain('.json');
  });
});
