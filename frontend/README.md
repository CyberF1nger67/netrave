# NetRave — React frontend

Интерфейс NetRave на React + TypeScript + Vite + Tailwind CSS.
Сборка кладётся в `../internal/web/static` и встраивается в бинарник через `//go:embed static`.

## Команды

```bash
npm install
npm run dev        # Vite, прокси /api и /ws на NETRAVE_BACKEND (по умолчанию 127.0.0.1:8080)
npm run typecheck
npm test
npm run build      # сборка в ../internal/web/static
npm run e2e        # нужен запущенный сервер и NETRAVE_PASSWORD
```

## Контракт с backend

- cookie `netrave_session`, заголовок `X-CSRF-Token`, 401 → экран входа;
- маршруты в браузерном адресе (например, `/labs`), сервер отдаёт `index.html` для них;
- WebSocket: `/ws/serial/{rid}`, `/ws/vnc/{rid}`, `/ws/notifications`;
- сложные движки редактора, терминала, VNC и дока консолей сохранены как
  императивные TypeScript-модули и обёрнуты React-страницами.

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
- The selected language is stored in `localStorage` (`netrave.language`) and is
  applied without a page reload.
