# NetRave — React frontend

The NetRave interface built with React + TypeScript + Vite + Tailwind CSS.
The build goes to `../internal/web/static` and is embedded into the binary via `//go:embed static`.

## Commands

```bash
npm ci
npm run dev        # Vite, proxies /api, /ws and /img to NETRAVE_BACKEND (default 127.0.0.1:8080)
npm run typecheck
npm test
npm run build      # build into ../internal/web/static
npm run e2e        # needs a running server and NETRAVE_PASSWORD
```

## Backend contract

- cookie `netrave_session`, header `X-CSRF-Token`, 401 → sign-in screen;
- routes in the browser address (for example `/labs`); the server returns `index.html` for them;
- WebSocket: `/ws/serial/{rid}`, `/ws/vnc/{rid}`, `/ws/notifications`;
- the complex engines of the editor, terminal, VNC and console dock are kept as
  imperative TypeScript modules and wrapped by React pages.

## Localization

- English is the default language; it is not inferred from the browser.
- The language switch is in the top bar (globe icon) and on the login screen.
- Translations live in `locales/*.yaml` (`locales/ru.yaml` for Russian) as
  `"English key": "translation"` lines; keys are English source strings, so
  English needs no file. `vite-plugins/yaml-locales.ts` parses the files at build
  time into the `virtual:locales` module, and the bundle ships only plain objects.
  To add a text: put the English string into `EN_KEYS` in `src/i18n/index.ts` and
  use `t('English string')` in the components, then add the translation to
  `locales/ru.yaml` (keep `{placeholder}` names as they are); without a
  translation line the English key is shown and `src/i18n/locales.test.ts` fails.
- Messages from the server are English keys with `{name}` parameters
  (`msgs.New`, `httpx.ErrKey`, warnings as `msgs.Message`); the interface translates
  them with `translateMessage(key, params)`. A parameter named `*_key` is itself a key
  and is translated with the same parameters (for example `{kind_key}`). Logs written
  to the server log stay in English. `python3 tools/check-i18n.py` (also run by
  `make test`) checks that every message of the Go code is in `EN_KEYS` and `ru.yaml`.
- The selected language is stored in `localStorage` (`netrave.language`) and is
  applied without a page reload.
