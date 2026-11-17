import { LogoMark } from '../Logo';
import { t } from '../../i18n';

/** Waiting screen while the session is checked. Private content is not rendered until then. */
export function Splash() {
  return (
    <div className="grid min-h-dvh place-items-center bg-bg text-text">
      <div className="text-center">
        <div className="flex items-center justify-center gap-2.5">
          <span className="grid h-9 w-9 place-items-center rounded-xl bg-gradient-to-br from-accent to-accent-2">
            <LogoMark size={22} />
          </span>
          <span className="text-lg font-extrabold tracking-tight">NetRave</span>
        </div>
        <div className="mt-3 text-sm text-muted">{t('Loading…')}</div>
      </div>
    </div>
  );
}
