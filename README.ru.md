[English](README.md) | [Русский](README.ru.md)

# NetRave

NetRave — self-hosted платформа для виртуальных сетевых лабораторий. Она
запускает машины QEMU/KVM, строит топологии, соединяет виртуальные кабели,
управляет образами и шаблонами и предоставляет serial- и VNC-консоли в браузере.

Go-backend является единственным источником истины для API, прав доступа и
жизненного цикла QEMU/сетей. React-frontend встроен в тот же бинарник.

## Содержание

- [Возможности](#возможности)
- [Архитектура](#архитектура)
- [Поддерживаемые дистрибутивы](#поддерживаемые-дистрибутивы)
- [Поддерживаемые init-системы](#поддерживаемые-init-системы)
- [Требования](#требования)
- [Быстрая установка install.sh](#быстрая-установка-installsh)
- [Установка из собранного бинарника](#установка-из-собранного-бинарника)
- [Сборка из исходников](#сборка-из-исходников)
- [Запуск в foreground](#запуск-в-foreground)
- [Демон](#демон)
- [Конфигурация](#конфигурация)
- [Структура данных](#структура-данных)
- [CLI](#cli)
- [Диагностика](#диагностика)
- [Логи](#логи)
- [Удаление](#удаление)
- [Безопасность](#безопасность)
- [Устранение неполадок](#устранение-неполадок)
- [Тестирование](#тестирование)
- [Вспомогательные скрипты tools](#вспомогательные-скрипты-tools)
- [Как внести вклад](#как-внести-вклад)
- [Авторство кода](#авторство-кода)
- [Лицензия](#лицензия)

## Возможности

- Каталог лабораторий с папками, drag-and-drop, экспортом и правами на каждую лабораторию.
- Визуальный редактор топологии: устройства, сети, кабели, масштаб и выделение.
- Управление QEMU/KVM: запуск, остановка, перезапуск, пауза, продолжение, wipe, клонирование,
  сохранение остановленной машины как образа.
- Папки образов, overlay-диски, CD-ROM и QEMU-параметры образов.
- Serial-терминал (xterm.js), VNC-клиент, общий док консолей и отдельная страница консолей.
- Каталог шаблонов, библиотека PNG-иконок и пользовательские темы.
- Сообщения администраторов всем или выбранным пользователям, доставляются в центр уведомлений.
- Лаборатории можно открыть всем пользователям на просмотр.
- Слияние одинаковых страниц памяти хоста (KSM или UKSM) и сетевая диагностика.
- Пользователи, роли, глобальные права, права на лаборатории и журнал аудита.
- Центр уведомлений в реальном времени через WebSocket с fallback-опросом.
- Dashboard с живыми счётчиками CPU, ОЗУ, диска и процессов QEMU.
- Английский и русский интерфейс. По умолчанию английский; переключатель языка
  находится в верхней панели (иконка планеты) и на экране входа.
- Интеграция с systemd, OpenRC и runit.
- Управляющий канал демона через Unix-сокет, структурированные логи и диагностика.

## Архитектура

| Область | Расположение |
|---|---|
| HTTP API и WebSocket-сервер | `internal/api`, `internal/console`, `internal/notify` |
| Основная логика лабораторий, устройств и сетей | `internal/labs`, `internal/devices`, `internal/network`, `internal/qemu` |
| Демон и IPC | `internal/daemon` |
| Определение дистрибутива | `internal/platform` |
| Адаптеры init-систем | `internal/init` |
| Установщик | `internal/installer` |
| CLI | `internal/cli` |
| Встроенный frontend | `internal/web/static` |
| Исходники React | `frontend/src` |
| Хранение данных и журнал событий | `internal/database`, `internal/labfile`, `internal/fstree`, `internal/journal` |

Демон не дублирует бизнес-логику. Он контролирует процесс HTTP-сервера,
перезапускает его после сбоев и предоставляет небольшой управляющий API через
Unix-сокет. Вся бизнес-логика остаётся в HTTP-сервере.

## Поддерживаемые дистрибутивы

| Дистрибутив | Семейство | Пакетный менеджер | Init-адаптеры |
|---|---|---|---|
| Debian | Debian | apt-get | systemd |
| Ubuntu | Debian | apt-get | systemd |
| RHEL | Red Hat | dnf | systemd |
| Fedora | Red Hat | dnf | systemd |
| Rocky Linux | Red Hat | dnf | systemd |
| AlmaLinux | Red Hat | dnf | systemd |
| Void Linux | Void | xbps-install | runit, OpenRC |
| Alpine Linux | Alpine | apk | OpenRC, runit |
| Arch Linux | Arch | pacman | systemd |
| Manjaro | Arch | pacman | systemd |

Определение выполняется через `/etc/os-release`. Данные дистрибутивов
зарегистрированы в `internal/platform/platform.go`; добавление нового
дистрибутива требует только новой записи адаптера. Определение init-системы
выполняется отдельно и может быть переопределено через `--init`.

## Поддерживаемые init-системы

| Init | Интеграция |
|---|---|
| systemd | `/etc/systemd/system/netrave.service`, `systemctl`, journald |
| OpenRC | `/etc/init.d/netrave`, `rc-update`, `rc-service` |
| runit | `/etc/sv/netrave`, `sv`, symlink сервиса в `/var/service` или `/etc/service` |

## Требования

- Linux на x86_64 или arm64 (aarch64). Виртуальные машины работают с KVM и используют
  эмулятор архитектуры хоста: `qemu-system-x86_64` или `qemu-system-aarch64`.
  Машины другой архитектуры с KVM не запускаются.
- Go 1.26.11 или новее для сборки backend (версия закреплена в `go.mod`).
- Node.js 20.19+ или 22.12+ и npm для сборки frontend (требование Vite).
- QEMU/KVM: `qemu-system-x86_64` (или `qemu-system-aarch64`), `qemu-img`, UEFI-прошивка (OVMF или AAVMF).
- Сеть: `ip`, `nftables`, `dnsmasq` для NAT и DHCP.
- root для TAP-мостов, nftables и сети KVM.
- `/dev/kvm` для аппаратного ускорения.

## Быстрая установка install.sh

Один скрипт ставит зависимости, собирает frontend и бинарник и регистрирует
сервис (systemd, OpenRC или runit):

```bash
sudo ./install.sh                      # установка или обновление
sudo ./install.sh --addr :9090         # другой HTTP-адрес
sudo ./install.sh --no-deps            # без установки системных пакетов
sudo ./install.sh -y                   # без вопросов о подтверждении
sudo ./install.sh --uninstall          # удалить, данные сохранить
sudo ./install.sh --uninstall --purge  # удалить вместе с данными (спросит подтверждение)
```

Ставятся только недостающие пакеты; систему целиком скрипт не обновляет. Go и
Node.js берутся из системы, если версии подходят; иначе официальные архивы
(проверка SHA-256) распаковываются в `/opt/netrave/toolchain`. Сборка
выполняется от имени пользователя, вызвавшего sudo, поэтому в репозитории не
появляются файлы с владельцем root. Повторный запуск сохраняет существующую
конфигурацию (`/etc/netrave/config.yaml`) и перезапускает работающий сервис с
новым бинарником. В конце установщик выводит логин и пароль администратора
`admin`; пароль показывается один раз.

Поддерживаемые семейства: Debian/Ubuntu, RHEL/Fedora/Rocky/Alma, Arch/Manjaro,
Void и Alpine. Для других дистрибутивов используйте `--no-deps` и установите
пакеты вручную.

## Установка из собранного бинарника

```bash
sudo ./netrave install --init systemd
```

Команда копирует текущий исполняемый файл в `/usr/local/bin/netrave`, создаёт
`/etc/netrave/config.yaml`, каталоги данных и логов, устанавливает сервис,
включает автозапуск и запускает его. Для OpenRC или runit используйте
`--init openrc` или `--init runit`.

## Сборка из исходников

```bash
cd frontend
npm ci
npm run build        # результат: ../internal/web/static
cd ..
go build -o netrave .
```

Frontend встраивается через `//go:embed static`, поэтому после каждой сборки
frontend нужно пересобрать Go-бинарник. Собранные файлы frontend в репозитории
не хранятся: корневой `.gitignore` исключает `internal/web/static/assets/`,
`index.html`, `console.html` и `img/`. Соберите frontend до сборки Go или
запустите `./install.sh`, который делает оба шага.

Makefile повторяет эти шаги:

```bash
make            # frontend и backend
make test       # go test, typecheck и unit-тесты frontend
make check      # test + gofmt + go vet
make install    # сборка и sudo ./netrave install
make clean
```

## Запуск в foreground

```bash
sudo ./netrave serve -addr :8080 -data /var/lib/netrave
```

Запуск `netrave` без аргументов тоже стартует сервер в foreground.

При первом запуске создаётся `admin` со случайным паролем. Установщик выводит его
в конце установки, а сервис также записывает его в `/var/log/netrave/netrave.log`.

Откройте `http://SERVER:8080/`.

Для разработки только интерфейса без root:

```bash
./netrave serve -addr 127.0.0.1:8080 -data /tmp/netrave-dev
cd frontend
NETRAVE_BACKEND=http://127.0.0.1:8080 npm run dev
```

Затем откройте `http://127.0.0.1:5173/`.

## Демон

```bash
sudo netrave daemon --config /etc/netrave/config.yaml --foreground
sudo netrave daemon --config /etc/netrave/config.yaml --background
```

- Foreground-режим используют systemd, OpenRC и runit.
- Background-режим отсоединяется от терминала. Пока демон работает, его PID
  записан в `run_dir` (`/run/netrave/daemon.pid`).
- Lock-файл запрещает запуск нескольких демонов: `/run/netrave/daemon.lock`.
- IPC-сокет: `/run/netrave/daemon.sock` (режим 0660, проверка peer credentials).
- После неожиданного выхода демон перезапускает HTTP-процесс с настраиваемой
  задержкой (`restart_sec`).
- SIGTERM и SIGINT останавливают дочерний процесс корректно; через 90 секунд
  группа процессов завершается через SIGKILL.

## Конфигурация

Пример (`/etc/netrave/config.yaml`):

```yaml
addr: ":8080"
data_dir: /var/lib/netrave
disks_dir: /var/lib/netrave/disks
labs_dir: /var/lib/netrave/labs
icons_dir: /var/lib/netrave/icons
db_path: /var/lib/netrave/netrave.db
socket_path: /run/netrave/daemon.sock
log_file: /var/log/netrave/netrave.log
restart_sec: 2
qemu_path: qemu-system-x86_64
```

Также конфигурация понимает ключи `instances_dir`, `logs_dir`, `themes_dir`,
`run_dir`, `tmp_dir`, `stop_vms_on_exit`, `init_system`, `service_name` и `command`.
Раздел `settings` записывается интерфейсом и содержит `session_ttl_hours`,
`max_upload_mb` и `role_admin_description`.

Приоритет: флаги командной строки, затем конфигурационный файл, затем
переменные окружения, затем встроенные значения по умолчанию.

Переменные окружения используют префикс `NETRAVE_`: `NETRAVE_ADDR`,
`NETRAVE_DATA`, `NETRAVE_DISKS`, `NETRAVE_INSTANCES`, `NETRAVE_LABS`, `NETRAVE_LOGS`,
`NETRAVE_ICONS`, `NETRAVE_DB`,
`NETRAVE_QEMU`, `NETRAVE_SOCKET`, `NETRAVE_LOG`, `NETRAVE_CONFIG`,
`NETRAVE_INIT`, `NETRAVE_SERVICE`.

## Структура данных

Всё хранится в каталоге `data_dir` (после установки это `/var/lib/netrave`):

- `netrave.db`: пользователи, роли, права, экземпляры лабораторий и их машины.
- `labs/`: лаборатории в виде файлов в дереве папок. Каталог — это папка,
  а `<имя>.yaml` внутри него — лаборатория.
- `images/` и `templates/`: записи каталога, по одному YAML-файлу на запись.
- `disks/`: папки с дисками и ISO-файлы (`disks_dir`).
- `instances/<владелец>/<лаба>/<устройство>/`: рабочие диски машин пользователей.
- `logs/<владелец>/<лаба>/<устройство>.log`: лог QEMU каждой машины.
- `icons/` и `themes/`: загруженные значки и темы.
- `journal.jsonl`: журнал событий, который показывается на странице «Логи».

Правила:

- Работающую лабораторию нельзя переименовать. Работающую машину нельзя
  переименовать и сменить ей значок. Сначала остановите машины.
- При переименовании лаборатории или машины её рабочие диски переносятся, а логи QEMU удаляются.
- При удалении пользователя его машины останавливаются, а рабочие файлы и логи удаляются.
- Базы и данные прежних версий не обновляются: их нужно пересоздать.

## CLI

```text
netrave serve       Запуск HTTP-сервера в foreground (по умолчанию)
netrave daemon      Запуск демона-супервизора
netrave status      Состояние демона или сервиса (IPC)
netrave start       Запуск установленного сервиса
netrave stop        Остановка установленного сервиса
netrave restart     Перезапуск установленного сервиса
netrave install     Установка бинарника, конфигурации, сервиса и автозапуска
netrave uninstall   Удаление сервиса и файлов программы
netrave doctor      Диагностика платформы, init и зависимостей
netrave netdiag     Состояние TAP, мостов, nftables и br_netfilter (JSON)
netrave logs        Просмотр логов демона
netrave version     Версия и метаданные сборки
netrave help        Справка; флаги команды: "netrave <команда> -h"
```

Примеры:

```bash
netrave status --json
netrave logs -n 200
sudo netrave doctor
sudo netrave start --init systemd
```

## Диагностика

```bash
sudo netrave doctor
sudo netrave doctor --json
sudo netrave netdiag
```

Отчёт `doctor` содержит дистрибутив, семейство, архитектуру, определённую
init-систему, статус root, доступность KVM, `ip_forward`, необходимые утилиты и
подсказки по установке пакетов. `netdiag` выводит состояние сетевых ресурсов в
JSON.

## Логи

- Журнал событий: страница «Логи» в веб-интерфейсе или `GET /api/logs`. Хранится в `journal.jsonl`.
- Логи машин: `logs/<владелец>/<лаба>/<устройство>.log`. Хвост лога показывается, когда машина не запускается.
- Демон: `/var/log/netrave/netrave.log`, `journalctl -u netrave -f`, `netrave logs -n 200`.
- Логи демона структурированы в JSON (`log/slog`).

## Удаление

```bash
sudo netrave uninstall
```

Останавливает и отключает сервис, удаляет unit или init-скрипт и бинарник.
Конфигурация и пользовательские данные сохраняются. Полное удаление:

```bash
sudo netrave uninstall --purge --yes
```

`--purge` удаляет `/etc/netrave`, `/var/lib/netrave` и `/var/log/netrave`.

## Безопасность

- Сервис работает от root только потому, что TAP, мосты, nftables и KVM требуют этих прав.
- Права IPC-сокета — `0660`; UID подключающегося процесса проверяется через `SO_PEERCRED`.
- Cookie сессии: `HttpOnly`, `SameSite=Strict`, `Secure` при TLS.
- Изменяющие API-запросы требуют заголовок `X-CSRF-Token` и проверку Origin.
- HTTP-сервер отправляет строгий Content-Security-Policy.
- Загрузки проверяются на клиенте и на сервере.
- Секреты не пишутся в логи.
- Файл конфигурации создаётся с правами `0640`, каталоги данных — с `0750`;
  unit systemd — с `0644`.

## Устранение неполадок

| Проблема | Решение |
|---|---|
| `address already in use` | Найдите слушателя: `sudo ss -ltnp \| grep :8080`; остановите его или используйте другой `-addr`. |
| Машины QEMU не запускаются | Проверьте `netrave doctor`, `/dev/kvm`, доступ к KVM и root. |
| NAT или DHCP недоступны | Установите `nftables` и `dnsmasq`; forwarding включается при создании NAT-сети. |
| Изменения frontend не видны | Выполните `npm run build` и пересоберите `netrave`. |
| Сервис не запускается | Проверьте `journalctl -u netrave` или `netrave logs`. |
| Неизвестная init-система | Укажите `--init systemd`, `--init openrc` или `--init runit`. |

## Тестирование

```bash
make test     # check-i18n.py, go test ./..., проверка типов и unit-тесты frontend
make check    # make test + gofmt -l . + go vet ./...
cd frontend
npm run e2e   # нужен запущенный сервер и NETRAVE_PASSWORD; без него тесты пропускаются
```

## Вспомогательные скрипты tools

Каталог `tools/` содержит диагностические и тестовые скрипты, которые не входят
в сборку:

- `check-i18n.py` (запускается через `make test`) проверяет, что все пользовательские
  сообщения Go-кода есть в `EN_KEYS` и в `frontend/locales/ru.yaml`.
- `instances-sql-check.py` проверяет все SQL-запросы из Go-кода по текущей схеме
  на in-memory SQLite.
- `l2-selftest.py` проверяет модель L2-сети на реальном ядре Linux без QEMU.
- `nat-shared-test.py` проверяет общую NAT-сеть: пул DHCP, изоляцию групп и
  доступ к шлюзу. Требует root, iproute2, nftables и dnsmasq.
- `live-cable-test.py` и `live_api_helper.py` тестируют кабели через HTTP API
  запущенного сервера. Адрес берётся из `NETRAVE_URL`.
- `net-diagnose.sh` выводит мосты, TAP-интерфейсы, правила nftables, процессы
  dnsmasq и сетевые интерфейсы запущенных машин QEMU. Запускать от root.

## Как внести вклад

1. Создайте отдельную ветку.
2. Сохраняйте совместимость API и прав доступа.
3. Добавляйте тесты на новое поведение.
4. Перед pull request запустите проверки backend и frontend.
5. Обновляйте оба README при изменении пользовательского поведения.

## Авторство кода

Код в этом репозитории сгенерирован с помощью DeepSeek v4.1 Flash и Claude
(модели Haiku и Sonnet).

## Лицензия

Copyright (C) 2026 CyberF1nger67 и SlaysBro

GPL-3.0-only. См. [LICENSE](LICENSE). Сторонние компоненты и тексты их лицензий
перечислены в [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
