import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { t } from '../../i18n';
import { ShellLayout } from '../../components/layout/ShellLayout';
import { findFolder, folderListQuery } from '../../routing/labLookup';
import { LabByNamePage } from './LabByNamePage';

/**
  * URL `/labs/<name>` with no second segment: it is either a folder (list page inside the shell),
  * or a lab at the root (editor without the shell). Root and folder names are unique, so
  * the match is unambiguous.
 */
export function LabOrFolderPage() {
  const name = useParams()['folderName'] ?? '';
  const folders = useQuery(folderListQuery);

  if (folders.isLoading) {
    return <div className="grid min-h-screen place-items-center bg-bg"><div className="text-muted">{t('Loading editor…')}</div></div>;
  }
  // Folder: a shell with contents via Outlet (the child route is the lab list).
  if (findFolder(folders.data ?? [], name)) return <ShellLayout />;
  return <LabByNamePage rootName={name} />;
}
