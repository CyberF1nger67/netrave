import { describe, expect, it } from 'vitest';
import type { LabFolder } from '../../types/api';
import { folderPath, subtreeIds } from './FolderDialogs';

const folders: LabFolder[] = [
  { id: 1, parent_id: null, name: 'A', lab_count: 0, created_at: 0, updated_at: 0 },
  { id: 2, parent_id: 1, name: 'B', lab_count: 0, created_at: 0, updated_at: 0 },
  { id: 3, parent_id: 2, name: 'C', lab_count: 0, created_at: 0, updated_at: 0 },
  { id: 4, parent_id: null, name: 'D', lab_count: 0, created_at: 0, updated_at: 0 },
];

describe('folder helpers', () => {
  it('builds full path', () => {
    expect(folderPath(folders, 3).map((folder) => folder.name)).toEqual(['A', 'B', 'C']);
    expect(folderPath(folders, 0)).toEqual([]);
  });
  it('collects subtree ids', () => {
    expect([...subtreeIds(folders, 1)].sort()).toEqual([1, 2, 3]);
    expect([...subtreeIds(folders, 4)]).toEqual([4]);
  });
});
