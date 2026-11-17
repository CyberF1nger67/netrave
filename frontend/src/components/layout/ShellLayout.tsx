import { useEffect, useState } from 'react';
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { canPage, useAuthStore } from '../../stores/auth';
import { useThemeStore } from '../../stores/theme';
import { useNotificationsStore } from '../../stores/notifications';
import { closeAllConsoles } from '../../features/consoles/registry';
import { closeAllConsoleSessions } from '../../features/consoles/runtime';
import { NotificationBell } from '../../features/notifications/NotificationBell';
import { Icon } from '../Icon';
import { LogoMark } from '../Logo';
import { LanguageSwitcher } from '../LanguageSwitcher';
import { t, useLanguageStore } from '../../i18n';
import { NAV_GROUPS, PAGES, pageKeyFromPath } from '../../routing/pages';
import { paths } from '../../routing/paths';
import { ChangePasswordDialog, useChangePassword } from '../../features/account/ChangePasswordDialog';

const NAV_KEY = 'netrave.navCollapsed';

/** Shared shell for authenticated sections: side menu, top bar and content via Outlet. */
export function ShellLayout() {
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);
  const themeState = useThemeStore((state) => state.state);
  const setStandard = useThemeStore((state) => state.setStandard);
  const resetTheme = useThemeStore((state) => state.reset);
  const stopNotifications = useNotificationsStore((state) => state.stop);
  const location = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem(NAV_KEY) === '1'; } catch { return false; }
  });
  const [drawer, setDrawer] = useState(false);
  const [menu, setMenu] = useState(false);
  const password = useChangePassword();
  useLanguageStore((state) => state.language);

  const currentKey = pageKeyFromPath(location.pathname);
  const pageTitle = t(currentKey ? PAGES[currentKey].title : 'Laboratories');

  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(false);
    document.addEventListener('click', close);
    return () => document.removeEventListener('click', close);
  }, [menu]);

  useEffect(() => {
    if (!drawer) return;
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') setDrawer(false); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [drawer]);

  const toggleNav = () => {
    const next = !collapsed;
    setCollapsed(next);
    try { localStorage.setItem(NAV_KEY, next ? '1' : '0'); } catch { /* ignore */ }
  };

  const toggleTheme = () => {
    const base = themeState.active === 'light' || themeState.active.startsWith('custom:')
      ? (document.documentElement.dataset['theme'] === 'light' ? 'dark' : 'light')
      : (themeState.active === 'dark' ? 'light' : 'dark');
    void setStandard(base);
  };

  const onLogout = async () => {
    closeAllConsoles();
    closeAllConsoleSessions();
    stopNotifications();
    resetTheme();
    await logout();
    navigate(paths.login, { replace: true });
  };

  if (!user) return null;

  return (
    <div className="flex min-h-dvh bg-bg">
      <aside className={`sticky top-0 z-[85] flex h-dvh flex-none flex-col border-r border-line bg-panel transition-[width] duration-300 ease-app max-[900px]:fixed max-[900px]:inset-y-0 max-[900px]:left-0 max-[900px]:h-full max-[900px]:w-[276px]! max-[900px]:shadow-[0_0_60px_rgba(0,0,0,.5)] max-[900px]:transition-transform max-[900px]:duration-300 ${collapsed ? 'w-[76px]' : 'w-[248px]'} ${drawer ? 'max-[900px]:translate-x-0' : 'max-[900px]:-translate-x-[104%]'}`}>
        <div className={`flex items-center gap-[11px] overflow-hidden whitespace-nowrap px-[18px] pb-[18px] pt-5 ${collapsed ? 'justify-center px-0 max-[900px]:justify-start max-[900px]:px-[18px]' : ''}`}>
          <span className="grid size-9 flex-none place-items-center rounded-[11px] bg-gradient-to-br from-accent to-accent-2 text-[17px] font-extrabold text-white shadow-[0_8px_20px_-8px_rgba(99,102,241,.7),inset_0_1px_0_rgba(255,255,255,.25)]"><LogoMark size={22} /></span>
          <span className={`text-[16.5px] font-bold tracking-[.01em] transition-opacity ${collapsed ? 'hidden max-[900px]:inline' : ''}`}>NetRave</span>
        </div>
        <nav className={`flex-1 overflow-y-auto overflow-x-hidden px-3 pb-2 pt-0.5 ${collapsed ? 'max-[900px]:px-3' : ''}`}>
          {NAV_GROUPS.map((group) => {
            const items = group.items.filter((key) => canPage(user, PAGES[key]));
            if (!items.length) return null;
            return (
              <div key={group.title} className={collapsed ? 'mb-2.5 max-[900px]:mb-4 [&+&]:border-t [&+&]:border-line [&+&]:pt-2.5 max-[900px]:[&+&]:border-t-0 max-[900px]:[&+&]:pt-0' : 'mb-4'}>
                <div className={`whitespace-nowrap px-2.5 pb-1.5 text-[10.5px] font-semibold uppercase tracking-[.09em] text-muted/85 ${collapsed ? 'hidden max-[900px]:block' : ''}`}>{t(group.title)}</div>
                {items.map((key) => {
                  const active = key === currentKey;
                  return (
                    <Link key={key} to={PAGES[key].path} title={t(PAGES[key].title)}
                      className={`relative my-0.5 flex items-center gap-3 whitespace-nowrap rounded-app-sm px-3 py-2.5 font-medium no-underline transition-[background,color,transform] duration-200 hover:translate-x-[3px] hover:bg-panel2 hover:text-text ${active ? "bg-accent-soft font-semibold text-text before:absolute before:-left-3 before:bottom-2.5 before:top-2.5 before:w-[3px] before:rounded-r before:bg-gradient-to-b before:from-accent before:to-accent-2 before:content-['']" : 'text-muted'} ${collapsed ? 'justify-center px-0 py-[11px] hover:translate-x-0 max-[900px]:justify-start max-[900px]:px-3 max-[900px]:py-2.5 max-[900px]:hover:translate-x-[3px]' : ''}`}
                      onClick={() => setDrawer(false)}>
                      <Icon name={PAGES[key].icon} className={active ? 'shrink-0 text-accent' : 'shrink-0'} />
                      <span className={`${collapsed ? 'hidden max-[900px]:inline' : ''}`}>{t(PAGES[key].title)}</span>
                    </Link>
                  );
                })}
              </div>
            );
          })}
        </nav>
        <button type="button" className="mx-3 mb-3.5 mt-2 min-h-9 border border-dashed border-line-2 bg-transparent text-muted hover:border-solid hover:text-text max-[900px]:hidden" title={t('Collapse / expand menu')} onClick={toggleNav}>
          <Icon name="chevron" className={`transition-transform duration-300 ${collapsed ? 'rotate-180' : ''}`} />
        </button>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-[62px] items-center gap-2.5 border-b border-line bg-panel px-6 max-[900px]:h-[58px] max-[900px]:gap-2 max-[900px]:px-3.5">
          <button type="button" className="hidden size-[38px] min-h-[38px] rounded-app-sm border-transparent bg-transparent p-0 text-muted hover:bg-panel2 hover:text-text max-[900px]:inline-flex" title={t('Menu')} onClick={() => setDrawer(true)}>
            <Icon name="menu" size={20} />
          </button>
          <div className="flex min-w-0 items-center gap-2 whitespace-nowrap text-sm">
            <span className="text-muted max-[900px]:hidden">NetRave</span><span className="text-line-2 max-[900px]:hidden">/</span><b>{pageTitle}</b>
          </div>
          <div className="flex-1" />
          <button type="button" className="border-transparent bg-accent-soft px-3.5 py-[7px] font-semibold text-text hover:border-transparent hover:bg-accent/24 hover:text-text max-[900px]:px-2.5" title={t('Running laboratories of all users')}
            onClick={() => navigate(paths.running)}>
            <Icon name="running" size={16} className="text-accent" /><span className="max-[900px]:hidden">{t('Running labs')}</span>
          </button>
          <LanguageSwitcher />
          <NotificationBell />
          <button type="button" className="size-[38px] min-h-[38px] rounded-app-sm border-transparent bg-transparent p-0 text-muted hover:bg-panel2 hover:text-text" title={t('Toggle theme')} onClick={toggleTheme}>
            <Icon name="moon" />
          </button>
          <div className="relative">
            <button type="button" className="min-h-10 rounded-full border border-line bg-transparent py-1 pl-1 pr-3 hover:bg-panel2 max-[900px]:p-[3px]" title={t('Profile')}
              onClick={(event) => { event.stopPropagation(); setMenu((value) => !value); }}>
              <span className="grid size-[30px] flex-none place-items-center rounded-full bg-gradient-to-br from-accent to-accent-2 text-[12.5px] font-bold text-white">{(user.username[0] ?? '?').toUpperCase()}</span>
              <span className="flex flex-col items-start text-left leading-[1.15] max-[900px]:hidden">
                <b className="text-[13px] font-semibold">{user.username}</b>
                <small className="text-[11px] text-muted">{user.role === 'admin' ? t('Administrator') : t('User role')}</small>
              </span>
            </button>
            <div className={`absolute right-0 top-[calc(100%+10px)] z-[90] min-w-[240px] origin-top-right rounded-app border border-line bg-panel p-1.5 shadow-app transition-[opacity,transform] duration-200 max-sm:min-w-[220px] ${menu ? 'visible scale-100 opacity-100' : 'invisible -translate-y-1.5 scale-[.98] opacity-0'}`} onClick={(event) => event.stopPropagation()}>
              <div className="flex flex-col px-2.5 pb-1.5 pt-2">
                <b>{user.username}</b>
                <small className="text-xs text-muted">{user.role === 'admin' ? t('Administrator') : t('User')}</small>
              </div>
              <hr className="mx-0.5 my-1.5 border-0 border-t border-line" />
              <button type="button" className="flex w-full items-center justify-start gap-2.5 rounded-app-sm border-0 bg-transparent px-2.5 py-[9px] text-left font-medium hover:bg-panel2" onClick={() => { setMenu(false); password.show(); }}>
                <Icon name="lock" size={16} /> {t('Change Password')}
              </button>
              <button type="button" className="flex w-full items-center justify-start gap-2.5 rounded-app-sm border-0 bg-transparent px-2.5 py-[9px] text-left font-medium hover:bg-panel2" onClick={() => { setMenu(false); toggleTheme(); }}>
                <Icon name="moon" size={16} /> {t('Toggle theme')}
              </button>
              <hr className="mx-0.5 my-1.5 border-0 border-t border-line" />
              <button type="button" className="flex w-full items-center justify-start gap-2.5 rounded-app-sm border-0 bg-transparent px-2.5 py-[9px] text-left font-medium text-bad hover:bg-bad/10" onClick={() => { setMenu(false); void onLogout(); }}>
                <Icon name="logout" size={16} /> {t('Logout')}
              </button>
            </div>
          </div>
        </header>
        <main className="min-w-0 flex-1 animate-page-in p-[26px_28px_40px] max-[900px]:p-[18px_14px_32px]"><Outlet /></main>
      </div>
      <div className={`max-[900px]:fixed max-[900px]:inset-0 max-[900px]:z-[84] max-[900px]:bg-[rgba(3,6,12,.55)] max-[900px]:backdrop-blur-[2px] max-[900px]:animate-fade-in ${drawer ? 'max-[900px]:block' : 'hidden'}`} onClick={() => setDrawer(false)} />
      <ChangePasswordDialog open={password.open} onClose={password.close} />
    </div>
  );
}
