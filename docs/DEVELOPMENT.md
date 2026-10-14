# Vlessia — для разработчиков

Как собрать, протестировать и выпустить Vlessia, и как он устроен внутри. Пользовательское описание — в [README](../README.md).

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
