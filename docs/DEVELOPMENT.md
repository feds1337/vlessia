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
| `main.go`, `app.go` | Запуск Wails, системный прокси и TUN, методы для UI, kill switch |
| `netwatch.go` | Переподключение при смене сети и после сна (десктоп) |
| `internal/core` | Общее для десктопа и Android: серверы и подписки, запуск подключения (`StartEngine`, цепочки и балансировка с health-check), пинг, счётчик трафика, сторож, напоминания о трафике и сроке (`notify.go`) |
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
| `frontend/dist` | Интерфейс (HTML/CSS/JS, без сборки); `i18n.js` — словари русского и английского |

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

### Kill switch

Галочка «Kill switch: блокировать интернет, когда VPN выключен» держит виртуальный адаптер включённым, пока VPN отключён, и отправляет всё в чёрную дыру: конфиг Xray с единственным дефолтным outbound `blackhole` (`xray.Options.Block` в `BuildConfig`); частные сети при этом остаются доступны. Состояние видно в `Status.Kill`, в интерфейсе — строка «Kill switch: интернет заблокирован».

- **Windows:** `startBlockSession` (`app_windows.go`) поднимает второй экземпляр Xray в процессе — с TUN-входом и blackhole — и настраивает адаптер через `netsh` (`tun.Configure`), как при обычном подключении. Нужны права администратора.
- **macOS и Linux:** тот же привилегированный помощник (`internal/tunhelper`), но с `--block`: его Xray шлёт всё из TUN в blackhole, а не в SOCKS клиента.
- **Android:** VPN-интерфейс не закрывается при отключении (`client.tunFd`), поэтому Android сам блокирует трафик, раз его никто не читает; служба VPN продолжает работать и показывает «Интернет заблокирован» (`Mobile.killSwitch()`).

`syncKill` в `app.go` сводит состояние: блокировка включается, когда VPN не подключён, а режим — TUN (или последнее подключение шло через TUN: полные туннели работают в TUN и в режиме «Прокси»), и снимается перед подключением. При старте без автоподключения блокировка включается сразу — иначе обещание «не утечёт» нарушалось бы до первого подключения.

### Цепочка (мультитуннель)

У сервера есть поле `Via` (`profile.Server.Via`) — ID второго сервера. В `core.StartEngine` релей разрешается в IP, при необходимости закрепляется его сертификат Hysteria, а если релей — полный туннель (WireGuard/AmneziaWG/OpenVPN), поднимается второй мост (`opts.ViaBridgePort`, `Engine.bridge2`). В конфиге Xray появляется второй outbound `via`, а у основного выставляется `outboundTag: "via"` — цепочку строит сам Xray. Адреса и основного сервера, и релея попадают в `Engine.Bypass`, чтобы маршруты к ним не уходили в туннель. Выбранный релей переживает обновление подписки: `replaceSubscriptionServers` переносит `Via` по исходной ссылке.

### Балансировка

Галочка «Балансировка между серверами подписки» включает `xray.Options.Balance`: серверы одной подписки (кроме полных туннелей) становятся outbound'ами `bal-1…N`, а тег `proxy`, на который ссылаются правила маршрутизации, объявляется в `routing.balancers`. Правила используют **`balancerTag`**, а не `outboundTag`: на балансировщик, записанный в `outboundTag`, Xray отвечает `non existing outTag` и рвёт соединения. В конце идёт catch-all правило `{network: "tcp,udp", balancerTag: "proxy"}` — иначе «остальное» уходило бы в первый outbound группы, то есть в один сервер.

Адреса серверов резолвятся заранее, по одному на outbound (`BalanceAddrs`): после поднятия TUN системный DNS ушёл бы в туннель. Если в группе меньше двух серверов, клиент тихо подключается напрямую.

**Health-check.** Перед подключением `healthyGroup` (`internal/core/engine.go`) параллельно пингует группу и выкидывает серверы, которые не ответили: мёртвый член группы иначе забирал бы свою долю новых соединений. Протоколы, которые нельзя пингуать (-2), остаются.

**Стратегии.** `balanceStrategy` в настройках: пусто/`roundrobin` — поровну; `leastping` — самый быстрый по данным observatory. Для второй в конфиг добавляется `burstObservatory` (subjectSelector `bal`, проба по `https://www.gstatic.com/generate_204` раз в минуту) и `fallbackTag: bal-1` — без него leastping до первой пробы возвращает пустой тег и **все** соединения падают. Пакет `transport/internet/tagged/taggedimpl` обязан быть импортирован (`features.go`), иначе `tagged.Dialer` остаётся nil и observatory падает с nil dereference.

### Правила по приложениям

`Settings.ProxyApps`/`DirectApps` превращаются в правила `{"type":"field","process":[…],"outboundTag":…}` и ставятся **первыми** (после перехвата DNS): выбор приложения важнее правил для сайтов. Xray ищет владельца соединения по системной таблице TCP (`find_process_*.go` в xray-core), поэтому это работает на Windows, Linux, macOS и Android. Запись вида `chrome.exe` (или `chrome`) матчится по имени процесса, `C:/Games/` — по папке, `C:\Program Files\Firefox\firefox.exe` — по полному пути. На Android вместо этого — раздельный туннель средствами VpnService.

### Раздельный туннель на Android

`Settings.AndroidApps`/`AndroidAppsMode` (`allow` — только выбранные через VPN, `block` — все, кроме выбранных, пусто — все). `client.tunConfig()` отдаёт это JSON-объектом в `Host.OpenTun(dns, config)`, а `VlessiaVpnService.openInterface` вызывает `addAllowedApplication` или `addDisallowedApplication`: смешивать их нельзя, поэтому список только один. Само приложение всегда вне туннеля — ему нужен интернет для подписок и обновлений. Список приложений отдаёт `Host.InstalledApps` (launcher-приложения через `PackageManager`; на Android 11+ нужен `<queries>` в манифесте).

### Переподключение при смене сети и после сна

`netwatch.go` раз в 5 секунд сравнивает «подпись» сети: имена и IPv4-адреса поднятых интерфейсов без loopback, link-local и собственного TUN-адаптера (он появляется и исчезает вместе с подключением). Изменение должно продержаться два тика, чтобы не ловить моменты создания адаптера. Пробел в тиках больше 30 секунд означает сон — тогда переподключение сразу. Дальше — `reconnectIfActive`, но не чаще раза в 30 секунд.

### Уведомления о трафике и сроке

`internal/core/notify.go` после каждого обновления подписки (и через 5 секунд после запуска) проверяет остаток трафика и срок: 25%, 10%, «трафик закончился», 7 дней, 1 день, «истекла». Сработавшие предупреждения хранятся в `Subscription.Notified` (битовая маска) и приходят один раз — повторно только после того, как условие прошло (пополнение трафика или новый срок). Текст уходит в `Library.Remind`: на десктопе — системное уведомление Wails плюс всплывашка в окне, на Android — уведомление через `Host.Notify`.

### Матричный тест конфигов

`TestConfigMatrix` (`internal/xray/config_matrix_test.go`) собирает конфиг по сетке: протоколы × proxy/TUN × цепочка × балансировка (обе стратегии) × правила × приложения, плюс полные туннели и блокирующий режим. Каждый вариант прогоняется через настоящий парсер Xray (`loadConfig`) и проверяется `checkConfigRefs`: каждый `outboundTag` существует как outbound, каждый `balancerTag` — как балансировщик, селектор балансировщика матчит хоть один outbound. Оба бага 0.5.0 — «JSON валиден, но Xray его не понял» — ловятся именно так.

### Мобильный интерфейс

Телефонный вид включается классом `html.android` (его ставит `android.js`) в конце `style.css`: док становится компактной шапкой, но кнопка подключения остаётся в нём, как на десктопе; вкладки прилипают к верху, диалоги открываются снизу листом (с учётом `safe-area`). В строке сервера на телефоне остаётся только «Удалить» (и «Логин» у OpenVPN): «Через» и «Копировать» — десктопные действия. Кнопки видны всегда, потому что на тач-экране нет наведения; если имя длинное, строка переносится. Проверять удобно без телефона: `chrome --headless=new --window-size=500,900 --screenshot` на страницу с `harness.html` в `iframe` шириной 390 (Chrome форсирует окно не меньше 500 px, поэтому вьюпорт задаётся iframe'ом).

### Язык интерфейса

Интерфейс двуязычный: русский (по умолчанию) и английский. `frontend/dist/i18n.js` держит оба словаря, `t()` с подстановкой `{0}`, `plural()`/`unitPlural()` для склонений (в русском три формы, в английском две) и `trGo()` — перевод сообщений, которые приходят из Go. Статичный текст в `index.html` размечается атрибутами `data-i18n`, `data-i18n-ph`, `data-i18n-title`, `data-i18n-aria` и заменяется в `applyLang`. Выбор языка хранится в `localStorage` и дублируется в трей через `App.SetLang`. Важно: сообщения Go написаны по-русски, и новое пользовательское сообщение нужно добавить в `GO_EXACT` или `GO_TEMPLATES` в `i18n.js`, иначе в английском интерфейсе оно останется русским.

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
