import { useEffect, useRef, useState } from 'react';
import type { FormEvent, KeyboardEvent } from 'react';
import { useAuthStore } from '../../stores/auth';
import { ApiError } from '../../api/client';
import { Icon } from '../../components/Icon';
import { LanguageSwitcher } from '../../components/LanguageSwitcher';
import { errorText } from '../../lib/format';
import { t, useLanguageStore } from '../../i18n';
import { AuthBrand } from './AuthBrand';
import { AuthField } from './AuthField';
import { authUi } from './authTheme';

// Maps a failed sign-in to a message the user can act on. Status codes come
// from the backend: 401 bad credentials, 429 rate limit, 403 rejected origin.
function signInErrorMessage(cause: unknown): string {
  if (cause instanceof ApiError) {
    if (cause.status === 401) return t('Invalid username or password.');
    if (cause.status === 429) return t('Too many attempts. Try again later.');
    if (cause.status === 403) return t('The request was rejected. Reload the page and try again.');
    if (cause.status >= 500) return t('Server error. Try again later.');
  }
  const message = errorText(cause);
  return !message || message === 'Unauthorized' ? t('Invalid username or password.') : message;
}

export function LoginPage() {
  const login = useAuthStore((state) => state.login);
  useLanguageStore((state) => state.language);
  const sessionExpired = useAuthStore((state) => state.sessionExpired);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [capsLock, setCapsLock] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [mounted, setMounted] = useState(false);
  // Ref guard blocks a second submit fired before React re-renders (e.g. double Enter).
  const submittingRef = useRef(false);
  const userRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setMounted(true);
  }, []);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submittingRef.current) return;
    const name = username.trim();
    if (!name) {
      setError(t('Enter username'));
      userRef.current?.focus();
      return;
    }
    if (!password) {
      setError(t('Enter password'));
      passwordRef.current?.focus();
      return;
    }
    submittingRef.current = true;
    setError('');
    setBusy(true);
    try {
      await login(name, password);
      // On success the route changes and this screen unmounts, so busy stays on.
    } catch (cause) {
      setError(signInErrorMessage(cause));
      submittingRef.current = false;
      setBusy(false);
      passwordRef.current?.select();
    }
  };

  const trackCapsLock = (event: KeyboardEvent<HTMLInputElement>) => {
    setCapsLock(Boolean(event.getModifierState?.('CapsLock')));
  };

  const togglePassword = () => {
    setShowPassword((value) => !value);
    passwordRef.current?.focus();
  };

  const reveal = `transition-all duration-700 ease-out motion-reduce:transition-none ${mounted ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-3 motion-reduce:opacity-100 motion-reduce:translate-y-0'}`;

  return (
    <div className={`${authUi.root} relative overflow-hidden`}>
      {/* Decorative background: grid and soft colour glows. Hidden from assistive tech. */}
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 overflow-hidden">
        <div className="absolute inset-0 bg-[linear-gradient(to_right,rgba(148,163,184,0.07)_1px,transparent_1px),linear-gradient(to_bottom,rgba(148,163,184,0.07)_1px,transparent_1px)] [background-size:56px_56px] [mask-image:radial-gradient(ellipse_75%_65%_at_40%_45%,#000_20%,transparent_100%)]" />
        <div className="absolute -left-48 -top-56 h-[34rem] w-[34rem] rounded-full bg-[#6366f1] opacity-25 blur-[130px]" />
        <div className="absolute -bottom-56 -right-40 h-[32rem] w-[32rem] rounded-full bg-[#3b82f6] opacity-20 blur-[140px]" />
      </div>

      <div className="relative z-10 mx-auto flex min-h-dvh w-full max-w-[1440px] flex-col px-4 pb-6 pt-4 sm:px-8 lg:px-12 lg:pb-8 lg:pt-7">
        <header className="flex items-center justify-between gap-4">
          <AuthBrand />
          <LanguageSwitcher tone="auth" />
        </header>

        <main className="grid flex-1 place-items-center py-8 sm:py-10">
          <section className={`relative mx-auto w-full max-w-[440px] min-w-0 ${reveal} delay-100`}>
            <div className={authUi.panelCard}>
              <div aria-hidden="true" className="absolute inset-x-8 top-0 h-px bg-gradient-to-r from-transparent via-[#6366f1] to-transparent opacity-80" />
              <div aria-hidden="true" className="absolute -right-16 -top-16 h-40 w-40 rounded-full bg-[#6366f1] opacity-10 blur-3xl" />

              <div className="relative">
                <div className="mb-7">
                  <h1 className="m-0! mb-1.5! text-[25px]! font-bold! leading-tight! tracking-[-0.02em]! text-[#e7edf5]!">{t('Log in to NetRave')}</h1>
                  <p className="m-0 text-[14px] leading-relaxed text-[#8b99ad]">{t('Sign in to manage laboratories and machines.')}</p>
                </div>

                <form className="block" noValidate onSubmit={submit} aria-describedby={error ? 'auth-error' : undefined}>
                  <AuthField
                    id="auth-user"
                    ref={userRef}
                    label={t('Username')}
                    icon="user"
                    value={username}
                    autoComplete="username"
                    autoCapitalize="off"
                    spellCheck={false}
                    placeholder={t('Enter username')}
                    autoFocus
                    disabled={busy}
                    invalid={Boolean(error) && !username.trim()}
                    onChange={(event) => { setUsername(event.target.value); if (error) setError(''); }}
                  />

                  <div className="mt-5">
                    <AuthField
                      id="auth-pass"
                      ref={passwordRef}
                      label={t('Password')}
                      icon="lock"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      autoComplete="current-password"
                      placeholder={t('Enter password')}
                      disabled={busy}
                      onKeyDown={trackCapsLock}
                      onKeyUp={trackCapsLock}
                      onBlur={() => setCapsLock(false)}
                      onChange={(event) => { setPassword(event.target.value); if (error) setError(''); }}
                      trailing={
                        <button
                          type="button"
                          title={showPassword ? t('Hide password') : t('Show password')}
                          aria-label={showPassword ? t('Hide password') : t('Show password')}
                          aria-pressed={showPassword}
                          disabled={busy}
                          onClick={togglePassword}
                          className={authUi.toggleButton}
                        >
                          <Icon name={showPassword ? 'eyeOff' : 'eye'} size={18} className="inline-block shrink-0" />
                        </button>
                      }
                    />
                  </div>

                  {capsLock && (
                    <div className="mt-2.5 flex items-center gap-1.5 text-[12px] font-medium text-[#f5a524]">
                      <Icon name="warning" size={14} className="inline-block shrink-0" />
                      {t('Caps Lock is on')}
                    </div>
                  )}

                  {sessionExpired && !error && (
                    <div role="status" className={authUi.alertWarn}>
                      {t('Your session has expired. Sign in again to continue.')}
                    </div>
                  )}

                  {error && (
                    <div id="auth-error" role="alert" aria-live="assertive" className={authUi.alertError}>
                      <span className="mt-0.5 flex-none"><Icon name="alert" size={16} className="inline-block shrink-0" /></span>
                      <span className="min-w-0 break-words">{error}</span>
                    </div>
                  )}

                  <button type="submit" disabled={busy} className={authUi.submit}>
                    {busy ? (
                      <>
                        <span aria-hidden="true" className="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white motion-reduce:animate-none" />
                        {t('Checking…')}
                      </>
                    ) : (
                      <>
                        {t('Sign in')}
                        <Icon name="arrow" size={18} className="inline-block shrink-0" />
                      </>
                    )}
                  </button>
                </form>
              </div>
            </div>
          </section>
        </main>

        <footer className="text-center text-[11.5px] text-[#8b99ad] opacity-70">© NetRave</footer>
      </div>
    </div>
  );
}

