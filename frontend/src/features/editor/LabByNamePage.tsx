import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { labsApi } from '../../api/endpoints';
import { t } from '../../i18n';
import { EditorPage } from './EditorPage';
import { findFolder, findLab, folderListQuery, labListQuery } from '../../routing/labLookup';
import { paths } from '../../routing/paths';

/**
  * Opens the editor by a human-readable URL `/labs/<folder>/<lab>` or, for a lab at the root,
  * `/labs/<lab>` (then the name is passed in `rootName`).
 *
  * A newly created lab may appear in the list with a delay: the list request
  * could have started before its creation. Therefore, while the lab is not found, data
  * is requested again, and the "lab not found" message is shown only
  * after several failed attempts - it does not flash while a lab is being created.
 */
export function LabByNamePage({ rootName }: { rootName?: string } = {}) {
  const params = useParams();
  const [search] = useSearchParams();
  const queryClient = useQueryClient();
  const isRoot = rootName !== undefined;
  const folderName = isRoot ? '' : params['folderName'] ?? '';
  const labName = isRoot ? rootName : params['labName'] ?? '';
  const instanceId = Number.parseInt(search.get('instance') ?? search.get('i') ?? '', 10) || 0;
  const rounds = useRef(0);
  const [exhausted, setExhausted] = useState(false);

  const folders = useQuery(folderListQuery);
  const labs = useQuery(labListQuery);

  // `/labs/7/editor` - the numeric editor URL, built by notifications and running labs.
  const labIdFromUrl = /^\d+$/.test(folderName) && labName === 'editor' ? Number(folderName) : 0;
  const labById = useQuery({
    queryKey: ['lab', labIdFromUrl],
    queryFn: () => labsApi.get(labIdFromUrl),
    enabled: labIdFromUrl > 0,
  });

  const folder = isRoot ? undefined : findFolder(folders.data ?? [], folderName);
  const folderKnown = isRoot || (folders.isSuccess && folder !== undefined);
  const lab = folderKnown ? findLab(labs.data ?? [], folder?.id ?? 0, labName) : undefined;
  const loading = labIdFromUrl > 0 ? labById.isLoading : folders.isLoading || labs.isLoading;
  // While retries are in progress, the page waits silently: "not found" at that moment would be false.
  const searching = labIdFromUrl === 0 && !lab && !exhausted;

  useEffect(() => {
    if (labIdFromUrl > 0 || lab || exhausted) return;
    if (rounds.current >= 10) {
      setExhausted(true);
      return;
    }
    rounds.current += 1;
    const timer = window.setTimeout(() => {
      void queryClient.invalidateQueries({ queryKey: ['labs'] });
      void queryClient.invalidateQueries({ queryKey: ['folders'] });
    }, 400);
    return () => window.clearTimeout(timer);
  }, [labIdFromUrl, lab, folderName, labName, queryClient, exhausted]);

  if (loading || searching) {
    return <div className="grid min-h-screen place-items-center bg-bg"><div className="text-muted">{t('Loading editor…')}</div></div>;
  }

  if (labIdFromUrl > 0) {
    if (!labById.data) {
      return <div className="grid min-h-screen place-items-center bg-bg"><div className="text-bad">{t('Laboratory not found')}</div></div>;
    }
    return <EditorPage labIdOverride={labIdFromUrl} instanceOverride={instanceId || undefined} />;
  }

  if (!lab) {
    return (
      <div className="grid min-h-screen place-items-center bg-bg p-6 text-center">
        <div>
          <div className="text-bad">{t('Laboratory not found')}</div>
          <Link className="mt-3 inline-block text-accent" to={paths.labs}>{t('Go to laboratories')}</Link>
        </div>
      </div>
    );
  }
  return <EditorPage labIdOverride={lab.id} instanceOverride={instanceId || undefined} />;
}
