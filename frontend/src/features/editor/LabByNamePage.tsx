import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { t } from '../../i18n';
import { EditorPage } from './EditorPage';
import { findLab, labListQuery } from '../../routing/labLookup';
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
  const routeLab = params['labName'] ?? '';
  const labName = isRoot ? rootName : routeLab === 'editor' ? params['folderName'] ?? '' : routeLab;
  const instanceId = Number.parseInt(search.get('instance') ?? search.get('i') ?? '', 10) || 0;
  const rounds = useRef(0);
  const [exhausted, setExhausted] = useState(false);

  const labs = useQuery(labListQuery);

  const lab = findLab(labs.data ?? [], labName);
  const loading = labs.isLoading;
  const searching = !lab && !exhausted;


  useEffect(() => {
    if (lab || exhausted) return;
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
  }, [lab, labName, queryClient, exhausted]);

  if (loading || searching) {
    return <div className="grid min-h-screen place-items-center bg-bg"><div className="text-muted">{t('Loading editor…')}</div></div>;
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
  return <EditorPage labName={lab.name} instanceOverride={instanceId || undefined} />;
}
