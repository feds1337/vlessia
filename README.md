# Vlessia

Десктопный VPN-клиент для Windows на базе [Xray-core](https://github.com/XTLS/Xray-core).
Xray встроен как Go-библиотека, поэтому всё собирается в один `Vlessia.exe`.

## Установка

Скачайте `Vlessia-<версия>-setup.exe` со страницы [Releases](https://github.com/feds1337/vlessia/releases) и запустите. Установщик ставит клиент в `Program Files\Vlessia`, создаёт ярлыки и запись в «Приложениях» (удаление оттуда же; серверы и настройки в `%APPDATA%\Vlessia` при удалении сохраняются). Без установки можно запускать `Vlessia-<версия>-portable.exe`.

## Возможности

- Протоколы: **VLESS** (включая REALITY и XTLS Vision), VMess, Trojan, Shadowsocks / **Outline**, **Hysteria 2**, **WireGuard**, **AmneziaWG** (1.0 – 3.1), **OpenVPN**, SOCKS5 и HTTP(S)-прокси
- Транспорты: TCP (RAW), WebSocket, gRPC, XHTTP, HTTPUpgrade
- Hysteria 2: ссылки `hysteria2://` / `hy2://`, обфускация salamander, port hopping (`443,20000-30000`, `mport`), скорость (`upmbps` / `downmbps`). Для `insecure=1` сертификат сервера закрепляется (pin) при каждом подключении — Xray больше не поддерживает отключение проверки
- AmneziaWG / WireGuard: файлы `.conf` (кнопка «Открыть файл…» или вставка текста) и ссылки `vpn://` из приложения AmneziaVPN. Поддерживаются параметры AWG 3.x (`HeaderProtectionKey`, `RandomTrailers`, `DisableCookies`, таймеры и диапазоны). Такие серверы всегда работают в режиме полного туннеля (TUN)
- Импорт ссылок из буфера обмена и подписок (base64 или список ссылок)
- OpenVPN: файлы `.ovpn` со встроенными или внешними сертификатами, tls-auth / tls-crypt / tls-crypt-v2, UDP и TCP, логин и пароль (`auth-user-pass` — клиент спросит при подключении). Работает в режиме полного туннеля
- WireGuard: `.conf` и ссылки `wireguard://` / `wg://` (формат v2rayN, sing-box, Hiddify; параметры AmneziaWG в ссылке тоже понимаются)
- Outline: ключи `ss://` и динамические ключи `ssconf://` (перезагружаются при каждом подключении)
- Прокси: `socks://` (в том числе формат v2rayN), `socks5://`, `http://`, `https://` с логином и паролем. Адрес вида `http://хост:порт` без пути считается прокси, с путём — подпиской
- Информация о подписке: остаток трафика, срок действия, название, анонс провайдера, ссылки на сайт и поддержку. Читаются заголовки `subscription-userinfo`, `profile-title`, `announce`, `support-url`, `profile-web-page-url`, `profile-update-interval`, `Content-Disposition`, а также строки `#ключ: значение` в теле подписки
- Автообновление подписок с интервалом, который задаёт провайдер (по умолчанию — раз в 12 часов)
- Два режима работы:
  - **Системный прокси** — HTTP-прокси на `127.0.0.1:10809` прописывается в Windows; права администратора не нужны
  - **TUN** — виртуальный адаптер wintun, через который идёт весь трафик системы; нужны права администратора (клиент предложит перезапуститься)
- Локальные SOCKS5 `127.0.0.1:10808` и HTTP `127.0.0.1:10809` работают в обоих режимах
- Опция «российские домены напрямую»
- TCP-пинг серверов, проверка реального соединения, скорость в реальном времени
- Значок в трее: подключение и смена режима; закрытие окна сворачивает в трей
- Флаги стран в названиях серверов (в Windows нет флагов-эмодзи, поэтому встроен шрифт Twemoji Country Flags)
- Звук при подключении — синтезируется в коде (`internal/sound`), отключается в настройках
- Автозапуск вместе с Windows (сразу в трей) и автоподключение к выбранному серверу при запуске. Если клиент запущен от администратора, автозапуск оформляется заданием Планировщика с наивысшими правами — так TUN работает без запроса UAC; иначе используется обычная автозагрузка (только режим системного прокси)

## Сборка

Нужны Go 1.27+ и Wails CLI v2:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
```

```bash
wails build -clean -trimpath -ldflags "-s -w"
```

Результат — `build/bin/Vlessia.exe`. Иконка приложения генерируется из кода командой `go run ./tools/genicon`.

Установщик (нужен [NSIS](https://nsis.sourceforge.io/), `makensis` в PATH) — `build/bin/Vlessia-amd64-installer.exe`:

```bash
wails build -clean -trimpath -ldflags "-s -w -X main.version=v0.1.0" -nsis
```

Скрипт установщика — `build/windows/installer/project.nsi` (шаблон Wails, доработан: русский язык, закрытие запущенного клиента, удаление автозапуска и возврат системного прокси при деинсталляции через `Vlessia.exe --cleanup`).

### Релизы

Релиз собирается GitHub Actions (`.github/workflows/release.yml`) при публикации тега: тесты, сборка установщика и portable-версии, публикация на странице Releases. Тег с дефисом (`v0.2.0-beta`) публикуется как пре-релиз.

```bash
git tag v0.2.0
```

```bash
git push origin v0.2.0
```

Тесты. `E2E=1` включает сквозные тесты через локальные серверы VLESS, Hysteria 2 и прокси (нужен интернет). `OPENVPN_EXE=путь\к\openvpn.exe` включает тест против официального OpenVPN-сервера (он запускается в режиме `dev null` — без драйверов и изменений в системе; `openvpn.exe` можно взять из MSI командой `msiexec /a OpenVPN-*.msi /qn TARGETDIR=папка`):

```bash
go test ./internal/...
```

## Устройство

| Путь | Назначение |
|---|---|
| `main.go`, `app.go` | Запуск Wails, логика подключения, методы для UI |
| `tray.go` | Иконка и меню в трее |
| `internal/profile` | Модель сервера, парсинг ссылок и подписок |
| `internal/subscription` | Метаданные подписки (трафик, срок, анонс) |
| `internal/awg` | AmneziaWG/WireGuard на userspace-стеке (amneziawg-go + netstack) |
| `internal/openvpn` | OpenVPN-клиент ([go-openvpn](https://github.com/n0madic/go-openvpn)) на userspace-стеке |
| `internal/socks` | Локальный SOCKS5 (TCP + UDP), через который Xray ходит в туннели AWG и OpenVPN |
| `internal/hysteria` | QUIC-проба Hysteria 2: пинг и сертификат для pin |
| `internal/xray` | Генерация JSON-конфига Xray и запуск ядра в процессе |
| `internal/sysproxy` | Системный прокси Windows (реестр + WinINet) |
| `internal/tun` | wintun.dll (встроен в exe), настройка адаптера и маршрутов, повышение прав |
| `internal/store` | Настройки и серверы в `%APPDATA%\Vlessia\config.json` |
| `frontend/dist` | Интерфейс (HTML/CSS/JS, без сборки) |

### Как работает TUN

1. Определяется физический адаптер с маршрутом в интернет, адрес сервера заранее резолвится в IP.
2. Xray создаёт адаптер `Vlessia`; исходящие соединения Xray привязаны к физическому адаптеру (`sockopt.interface`), поэтому собственный трафик Xray не попадает обратно в туннель.
3. Через `netsh` адаптеру назначаются адрес, DNS и маршруты `0.0.0.0/1` + `128.0.0.0/1` (и IPv6 `::/1` + `8000::/1`). Все изменения временные и исчезают вместе с адаптером.
4. DNS-запросы, пришедшие в TUN, перехватываются и разрешаются DNS-модулем Xray через прокси.

Данные хранятся в `%APPDATA%\Vlessia` (путь можно переопределить переменной окружения `VLESSIA_DATA`). Там же лежат `app.log`, `xray.log` и последний сгенерированный `xray-config.json` — для отладки.

### Как работает AmneziaWG

Xray не умеет AmneziaWG, поэтому туннель поднимает [amneziawg-go v3](https://github.com/amnezia-vpn/amneziawg-go) в userspace-стеке. Наружу он отдаёт локальный SOCKS5, а Xray подключается к нему как к обычному outbound и сам обеспечивает TUN, маршрутизацию, DNS и статистику. UDP-сокет WireGuard привязан к физическому адаптеру, чтобы не уходить в собственный туннель.

Важно: AmneziaWG 3.x есть только в модуле `github.com/amnezia-vpn/amneziawg-go/v3`. Старый модуль без `/v3` (включая его master) не понимает `HeaderProtectionKey`/`RandomTrailers`, и сервер AWG 3 молча игнорирует такой клиент. Параметры из `.conf` переводятся в UAPI в `internal/awg` (`uapiKeys`), неизвестные пропускаются с записью в лог. Сохранённые AWG-серверы перечитываются из исходной ссылки при каждом запуске, поэтому новые параметры применяются без повторного импорта.

### Обход бага кэша Hysteria в Xray

Транспорт Hysteria в xray-core хранит клиентов в глобальном кэше по адресу сервера и никогда их не удаляет. Новый экземпляр Xray подхватил бы старые настройки TLS, авторизации и привязки к адаптеру. `internal/xray/hysteria_reset.go` очищает этот кэш через `go:linkname` при каждой остановке Xray. Если обновление xray-core изменит внутреннюю структуру, упадёт тест `TestHysteriaResetLayout`.

## Ограничения

- Только Windows.
- Параметр `allowInsecure` в Xray удалён (с 01.06.2026); вместо него поддерживаются параметры ссылки `pcs` (pinnedPeerCertSha256) и `vcn` (verifyPeerCertByName).
- Плагины Shadowsocks (`?plugin=`) не поддерживаются.
- AmneziaWG: параметры J1–J3 и Itime (промежуточная версия AWG 1.5) пропускаются — в amneziawg-go v3 их нет. Из ссылок `vpn://` импортируются только контейнеры AWG/WireGuard.
- WireGuard/AmneziaWG и OpenVPN по UDP не отвечают на пинг (протоколы молчат на неаутентифицированные пакеты) — их проверяют подключением.
- OpenVPN: не поддерживаются сжатие (`comp-lzo`, `compress`), старые шифры CBC (нужен AES-GCM или CHACHA20-POLY1305 — все серверы OpenVPN 2.5+ их согласуют), режим `dev tap`, статические ключи и профили без tls-auth / tls-crypt. Клиент сообщит об этом при импорте.
- Outline: маскировка `prefix` не поддерживается Xray — подключение работает, но без неё.

## Сторонние ресурсы

- `internal/tun/wintun.dll` — [Wintun](https://www.wintun.net/) 0.14.1 (amd64), © WireGuard LLC, распространяется вместе с клиентом по [лицензии Prebuilt Binaries](internal/tun/wintun-LICENSE.txt); клиент использует его только через штатный API.
- Шрифт флагов `frontend/dist/fonts/TwemojiCountryFlags.woff2` из пакета [country-flag-emoji-polyfill](https://github.com/talkjs/country-flag-emoji-polyfill) (TalkJS, MIT). Изображения флагов — [Twemoji](https://github.com/twitter/twemoji), © Twitter, Inc. и др., лицензия [CC-BY 4.0](https://creativecommons.org/licenses/by/4.0/). Полный текст: `frontend/dist/fonts/TwemojiCountryFlags-LICENSE.md`.

## Лицензия

Проект распространяется под лицензией [AGPL-3.0](LICENSE). В проект встроен [go-openvpn](https://github.com/n0madic/go-openvpn) под лицензией **AGPL-3.0**, поэтому собранный клиент тоже подпадает под AGPL-3.0: для личного использования ограничений нет, но при распространении клиента (или предоставлении его как сервиса) нужно открыть исходный код всего проекта. Остальные зависимости: Xray-core — MPL-2.0, amneziawg-go — MIT, Wails — MIT.
