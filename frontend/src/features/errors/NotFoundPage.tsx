import { Link } from 'react-router-dom';
import { paths } from '../../routing/paths';
import { t } from '../../i18n';

export function NotFoundPage() {
  return (
    <div className="grid min-h-[60vh] place-items-center p-6 text-center">
      <div className="max-w-md">
        <div className="text-6xl font-extrabold tracking-tight text-accent">404</div>
        <h1 className="mt-3 text-xl font-semibold">{t('Page not found')}</h1>
        <p className="mt-2 text-sm text-muted">{t('This page does not exist or has moved.')}</p>
        <Link to={paths.labs} className="mt-6 inline-flex items-center rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-white">
          {t('Go to laboratories')}
        </Link>
      </div>
    </div>
  );
}
