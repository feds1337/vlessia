# Vlessia — для разработчиков

Как собрать, протестировать и выпустить Vlessia, и как он устроен внутри. Пользовательское описание — в [README](../README.md).

## Сборка

Сборка для macOS и Linux делается на соответствующей системе (Wails использует системный WebView через cgo):

- **Windows:** WebView2 (есть в Windows 10/11), для установщика — NSIS.
- **macOS:** Xcode Command Line Tools. Universal-сборка: `wails build -platform darwin/universal`.
- **Linux:** `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config`, сборка с тегом `webkit2_41`: `wails build -tags webkit2_41`.

CI (`.github/workflows/ci.yml`) на каждый push проверяет, тестирует и собирает клиент на всех трёх системах и под Android.

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

### Android

Нужны Android SDK (platform 35, build-tools 35), NDK 27.2, JDK 17 и gomobile той же версии, что `golang.org/x/mobile` в `go.mod`:

```bash
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867 golang.org/x/mobile/cmd/gobind@v0.0.0-20261011034528-197770444867
```

Сначала Go-ядро собирается в `android/app/libs/vlessia.aar` (переменная `TARGETS=android/amd64` ускоряет сборку только под эмулятор), затем APK — Gradle сам копирует интерфейс из `frontend/dist`:

```bash
ANDROID_NDK_HOME=$ANDROID_HOME/ndk/27.2.12479018 bash android/build-core.sh
```

```bash
cd android && ./gradlew assembleDebug
```

APK по архитектурам и универсальный — в `android/app/build/outputs/apk/`. Релизная сборка (`assembleRelease -PvlessiaVersion=v0.3.0`) подписывается ключом из переменных `VLESSIA_KEYSTORE`, `VLESSIA_KEYSTORE_PASSWORD`, `VLESSIA_KEY_ALIAS`, `VLESSIA_KEY_PASSWORD`; в GitHub Actions они берутся из секретов `ANDROID_KEYSTORE_BASE64` (keystore в base64), `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`. Обновление ставится поверх старой версии, только если подпись та же, — ключ нельзя терять или менять.

### Релизы

Релиз собирается GitHub Actions (`.github/workflows/release.yml`) при публикации тега: тесты, сборка установщика, portable-версии, пакетов macOS и Linux и APK для Android, публикация на странице Releases. Тег с дефисом (`v0.2.0-beta`) публикуется как пре-релиз.

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
| `main.go`, `app.go` | Запуск Wails, системный прокси и TUN, методы для UI |
| `internal/core` | Общее для десктопа и Android: серверы и подписки, запуск подключения (`StartEngine`), пинг, счётчик трафика, сторож |
| `mobile` | Android-библиотека для gomobile: те же методы для UI, VPN-интерфейс от Android, автообновления |
| `android/` | Android-приложение на Kotlin: WebView с интерфейсом, VpnService, плитка, PackageInstaller OTA |
| `internal/qr` | Чтение QR-кодов из картинок и с экрана (gozxing, kbinani/screenshot) |
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

### Сторож соединения и быстрый сервер

`core.Watchdog` раз в 20 секунд делает HTTPS-запрос через локальный HTTP-вход Xray (`CheckThrough`: gstatic, затем Cloudflare). Две неудачи подряд при работающем интернете в обход VPN (`core.Online`: TCP до 1.1.1.1 / 77.88.8.8 / 8.8.8.8 через физический адаптер) — соединение считается мёртвым, и `App.recoverConnection` переподключается. Если и новое соединение умерло, ни разу не заработав, клиент переходит на самый быстрый другой сервер той же подписки (`core.Fastest` по пингу). Строка «Самый быстрый сервер» в начале списка серверов (`SetAutoBest`) делает такой выбор перед каждым подключением.

### Свои правила сайтов

`xray.ParseRules` превращает строки настроек в правила маршрутизации Xray: домен (с поддоменами, `*.` и URL тоже понимаются), префиксы Xray `domain:`, `full:`, `keyword:`, `regexp:`, IP и подсети. Порядок правил: DNS в TUN, частные сети напрямую, «через VPN», «напрямую», затем галочка про .ru. Если есть правила по IP, routing переключается на `IPIfNonMatch`, иначе доменные соединения по IP-правилам не совпали бы.

### Закрытие TUN-входа Xray

У TUN-входа xray-core нет метода `Close`: при остановке Xray его стек gVisor и поток чтения TUN продолжали бы работать, и каждое переподключение в режиме «Весь трафик» оставляло бы ещё один. `internal/xray/tun_close.go` закрывает стек сам через reflection; если внутренняя структура изменится, упадёт тест `TestTunStackLayout`.

### Обновления (OTA)

`internal/update` запрашивает `https://api.github.com/repos/feds1337/vlessia/releases/latest` и сравнивает тег с версией сборки (`-X main.version=…`; сборка `dev` не обновляется). Установщик релиза скачивается во временную папку и проверяется по размеру и по `SHA256SUMS.txt`, который публикует workflow релиза. Затем клиент отключает VPN (иначе системный прокси остался бы указывать на закрытый порт) и запускает установщик через `ShellExecute` (ему нужны права администратора):

```
Vlessia-vX.Y.Z-setup.exe /S --relaunch [--connect] /D=<папка установки>
```

`project.nsi` в `.onInstSuccess` разбирает `--relaunch` / `--connect` и запускает новую версию (с `--connect` — если до обновления VPN был подключён). Portable-копия (рядом нет `uninstall.exe`) обновляться на месте не может и открывает страницу релиза.

Пока идёт генерация привязок Wails (`wails build` запускает программу с тегом `bindings`), `main()` не трогает папку данных, логи и миграцию — см. `bindings_on.go`.

### «Весь трафик» на macOS и Linux

GUI-клиент работает от обычного пользователя: графические приложения от root на Linux под Wayland не запускаются, а на macOS это небезопасно. Для режима TUN клиент запускает свой же исполняемый файл с `--tun-helper` от root через `osascript` (macOS) или `pkexec` (Linux) — см. `internal/tunhelper`:

1. Клиент открывает служебный порт на 127.0.0.1 и передаёт помощнику одноразовый токен.
2. Помощник подключается, предъявляет токен, добавляет маршруты до VPN-серверов через физический шлюз, запускает Xray с TUN-входом (`utun99` / `vlessia0`) и выходом в SOCKS клиента и направляет `0.0.0.0/1`, `128.0.0.0/1`, `::/1`, `8000::/1` в туннель. DNS: на Linux через `resolvectl` (без systemd-resolved — маршруты к серверам из `/etc/resolv.conf`), на macOS — маршруты к серверам из `scutil --dns`; запросы на порт 53 в туннеле разрешает Xray через VPN.
3. Сообщает `ready`. Когда клиент закрывает соединение (отключение или падение), помощник удаляет маршруты и выходит; если завершится помощник — клиент отключается и показывает причину.

Исходящие соединения клиента привязаны к физическому адаптеру (`IP_BOUND_IF` на macOS, `SO_BINDTODEVICE` на Linux, ядро 5.7+), поэтому прямой трафик не попадает обратно в туннель.

### Как устроен Android

Интерфейс — тот же `frontend/dist` в WebView. `android/app/src/main/assets/web/android.js` подставляет вместо рантайма Wails `window.go.main.App.*` (промисы) и `window.runtime.EventsOn`; вызовы идут в `MainActivity` и дальше в `Mobile.call(метод, JSON)`, который по имени вызывает метод `mobile.client` (через reflection — список методов совпадает с десктопным `App`). События ядра приходят через `Host.emit`.

Подключение: ядро (`core.StartEngine`) резолвит сервер, поднимает мост AmneziaWG/OpenVPN, если нужно, и перед запуском Xray просит у Android VPN-интерфейс (`Host.openTun` → `VlessiaVpnService.establish`). Интерфейс получает маршруты `0.0.0.0/0` и `::/0`, а само приложение исключается из VPN (`addDisallowedApplication`), поэтому соединения Xray с серверами идут напрямую без `protect()`. Дескриптор передаётся TUN-входу Xray через переменную окружения `xray.tun.fd`. Xray этот дескриптор не закрывает, поэтому его закрывает ядро после остановки Xray — это и выключает VPN.

OTA-обновления на Android (`mobile/update.go` и `Core.kt`): `updateLoop` проверяет GitHub после старта и каждые 6 часов, автоматически скачивает APK под архитектуру устройства, сверяет `SHA256SUMS.txt` и запускает сессию `PackageInstaller` (`USER_ACTION_NOT_REQUIRED` на Android 12+ позволяет обновлять приложение без диалога, если Vlessia уже является установщиком пакета; иначе открывается системное подтверждение, а при первом обновлении — разрешение `ACTION_MANAGE_UNKNOWN_APP_SOURCES` с автоматическим продолжением установки при возврате в приложение).

«Постоянный VPN» Android запускает `VlessiaVpnService` без команды — служба подключается к выбранному серверу. Плитка в шторке (`VlessiaTile`) подключает и отключает.

Для проверки на эмуляторе удобно поднять на компьютере локальный сервер Xray (он доступен в эмуляторе как `10.0.2.2`), а интерфейс отлаживать через `chrome://inspect` — WebView отладочной сборки открыт для DevTools.

### Бенчмарки

```bash
go test ./internal/xray -run XXX -bench . -benchtime 3x -benchmem
```

`BenchmarkVLESS` — путь через Xray и VLESS, `BenchmarkAmneziaWG` и `BenchmarkAmneziaWGBridgeOnly` — через SOCKS-мост и AmneziaWG в userspace. Сервер AWG в бенчмарке работает в том же процессе, поэтому цифры ниже реальных.

### Разработческие сборки рядом с установленным клиентом

`-ldflags "-X main.instanceID=vlessia-dev"` даёт сборке отдельную блокировку единственного экземпляра, а `VLESSIA_DATA=<папка>` — отдельную папку данных, так что её можно запускать параллельно с установленным клиентом, не затрагивая его.
