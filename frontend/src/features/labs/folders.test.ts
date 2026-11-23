import { describe, expect, it } from 'vitest';
import type { LabFolder } from '../../types/api';
import { folderPath, subtreeIds } from './FolderDialogs';

const folders: LabFolder[] = [
  { path: 'A', parent: '', name: 'A', lab_count: 0, created_at: 0, updated_at: 0 },
  { path: 'A/B', parent: 'A', name: 'B', lab_count: 0, created_at: 0, updated_at: 0 },
  { path: 'A/B/C', parent: 'A/B', name: 'C', lab_count: 0, created_at: 0, updated_at: 0 },
  { path: 'D', parent: '', name: 'D', lab_count: 0, created_at: 0, updated_at: 0 },
];

describe('folder helpers', () => {
  it('builds full path', () => {
    expect(folderPath(folders, 'A/B/C').map((folder) => folder.name)).toEqual(['A', 'B', 'C']);
    expect(folderPath(folders, '')).toEqual([]);
  });
  it('collects subtree ids', () => {
    expect([...subtreeIds(folders, 'A')].sort()).toEqual(['A', 'A/B', 'A/B/C']);
    expect([...subtreeIds(folders, 'D')]).toEqual(['D']);
  });
});
