import { foldersApi, labsApi } from '../../api/endpoints';
import { t } from '../../i18n';
import { toast } from '../../stores/toast';

/** The same limit as on the server: the import request body is read up to 8 MB. */
export const MAX_IMPORT_BYTES = 8 << 20;

/** Extensions accepted by the file picker: a lab export is YAML only. */
export const IMPORT_ACCEPT = '.yaml,.yml,application/yaml,text/yaml';

/**
  * Imports a lab from an export file (YAML).
  * The server parses and validates the contents and issues new numbers; if a folder is given,
  * the lab is placed into it. Returns the id of the created lab. Errors are thrown
  * with user-facing text.
 */
export async function importLabFile(file: File, folderId: number): Promise<number> {
  if (file.size > MAX_IMPORT_BYTES) throw new Error(t('The file is too large (max. 8 MB)'));

  const text = await file.text();
  const result = await labsApi.importLab(text);
  if (folderId) await foldersApi.placeLab(result.id, folderId);

  const warnings = result.warnings?.length ?? 0;
  if (warnings > 0) toast.warning(`${t('Imported with warnings:')} ${warnings}`);
  else toast.success(t('Laboratory imported'));
  return result.id;
}
