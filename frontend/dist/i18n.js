"use strict";

// ---------- Vlessia UI strings ----------
// The client is bilingual: Russian (default) and English. Static markup
// localises itself through data-i18n attributes (applyLang); dynamic text
// goes through t(). Messages that come from the Go core (errors, reminders)
// are translated by trGo, which maps known Russian strings to English.
// New user-facing Go strings must be added to GO_TEMPLATES.

const I18N = {
  ru: {
    "status.connected": "Подключено",
    "status.connecting": "Подключение…",
    "status.disconnecting": "Отключение…",
    "status.disconnected": "Отключено",
    "dock.no_server": "Сервер не выбран",
    "dock.send": "Отправка",
    "dock.receive": "Получение",
    "dock.mode_aria": "Режим",
    "dock.proxy": "Прокси",
    "dock.tun": "Весь трафик",
    "dock.connect": "Подключить",
    "dock.disconnect": "Отключить",
    "dock.test": "Проверить соединение",
    "dock.testing": "Проверяю…",
    "dock.update": "Обновить",
    "dock.download": "Скачать",
    "dock.whats_new": "Что нового",
    "dock.detail_tun": "Весь трафик идёт через VPN",
    "dock.detail_proxy": "Прокси 127.0.0.1:{0}",
    "dock.detail_sub": "Подписка: {0}",
    "dock.detail_via": "через {0}",
    "dock.detail_balance": "балансировка: {0} {1}",
    "dock.err_need_admin": "Для режима «Весь трафик» нужны права администратора",
    "dock.err_need_auth": "Введите логин и пароль OpenVPN",
    "dock.kill": "Kill switch: интернет заблокирован",
    "dock.tun_locked": "WireGuard, AmneziaWG и OpenVPN работают только в режиме «Весь трафик»",
    "dock.version": "Версия {0}",
    "dock.version_dev": "Версия для разработки",
    "dock.admin": "права администратора",
    "dock.session": "За сессию {0}, сегодня {1}",
    "dock.today": "Сегодня через VPN {0}",
    "tab.servers": "Серверы",
    "tab.subs": "Подписки",
    "tab.settings": "Настройки",
    "tab.logs": "Журнал",
    "servers.search_ph": "Найти сервер",
    "servers.ping_title": "Проверить пинг серверов",
    "servers.update_subs_title": "Обновить все подписки",
    "servers.qr_title": "Найти QR-код сервера или подписки на экране",
    "servers.qr_android": "Сканировать QR-код сервера или подписки",
    "servers.qr_aria": "Добавить по QR-коду",
    "servers.add": "Добавить",
    "servers.paste": "Вставить",
    "servers.paste_title": "Добавить ссылку или подписку из буфера обмена",
    "servers.empty": "Добавьте первый сервер",
    "servers.empty_sub": "Скопируйте ссылку на сервер или подписку от провайдера и нажмите «Вставить».",
    "servers.free_bot": "Бесплатный VPN в @v4hood_bot",
    "servers.manual": "Добавленные вручную",
    "servers.not_found": "Ничего не найдено",
    "servers.not_found_sub": "Попробуйте другое название или адрес.",
    "servers.login": "Логин",
    "servers.copy": "Копировать",
    "servers.del": "Удалить",
    "servers.via": "Через",
    "servers.via_set": "через {0}",
    "ping.none": "нет ответа",
    "ping.ms": "{0} мс",
    "ping.na_title": "Этот протокол не отвечает на проверку — проверьте подключением",
    "sub.fallback": "Подписка",
    "sub.left": "осталось {0}",
    "sub.unlimited": "безлимит",
    "sub.expired": "истекла",
    "unit.day": ["день", "дня", "дней"],
    "unit.server": ["сервер", "сервера", "серверов"],
    "unit.server_added": ["сервер добавлен", "сервера добавлено", "серверов добавлено"],
    "auto.title": "Самый быстрый сервер",
    "auto.on": "Включено",
    "auto.now": "Сейчас {0}",
    "auto.meta_sub": "По пингу среди серверов подписки «{0}»",
    "auto.meta_manual": "По пингу среди добавленных вручную",
    "toast.copied_link": "Ссылка скопирована",
    "toast.copied_conf": "Конфигурация скопирована",
    "toast.server_deleted": "Сервер удалён",
    "toast.added": "{0} {1}",
    "toast.auto_on_conn": "Переподключаюсь к самому быстрому серверу",
    "toast.auto_on": "Перед подключением выберу самый быстрый сервер",
    "toast.login_saved": "Логин сохранён",
    "toast.test_ok": "Соединение работает, задержка {0} мс",
    "toast.no_subs": "Подписок пока нет — добавьте их на вкладке «Подписки»",
    "toast.subs_updated": "Подписки обновлены",
    "toast.saved": "Сохранено",
    "toast.via_set": "Цепочка: через {0}",
    "toast.via_off": "Цепочка отключена",
    "subs.url_ph": "Ссылка на подписку, https://…",
    "subs.url_aria": "Ссылка на подписку",
    "subs.name_ph": "Название (необязательно)",
    "subs.name_aria": "Название",
    "subs.add": "Добавить подписку",
    "subs.empty": "Подписок пока нет",
    "subs.empty_sub": "Вставьте ссылку на подписку от провайдера — серверы и остаток трафика будут обновляться сами.",
    "subs.site": "Сайт",
    "subs.support": "Поддержка",
    "subs.update": "Обновить",
    "subs.rename": "Переименовать",
    "subs.traffic_none": "Провайдер не сообщает расход трафика.",
    "subs.traffic_unl": "Безлимитный трафик",
    "subs.used": "использовано {0}",
    "subs.left": "Осталось {0}",
    "subs.of": "из {0}",
    "subs.forever": "Бессрочно",
    "subs.expired_on": "Истекла {0}",
    "subs.valid": "Действует до {0}, ещё {1} {2}",
    "subs.foot": "{0} {1}, обновлено {2}, автообновление каждые {3} ч",
    "subs.never": "ещё не обновлялась",
    "subs.err": "Не удалось обновить: {0}",
    "toast.sub_added": "Подписка добавлена: {0} {1}",
    "toast.sub_updated": "Подписка обновлена: {0} {1}",
    "toast.sub_deleted": "Подписка удалена",
    "s.connection": "Подключение",
    "s.mode": "Режим",
    "s.mode_proxy": "Прокси — браузеры и большинство программ",
    "s.mode_tun": "Весь трафик — нужны права администратора",
    "s.mode_tun_unix": "Весь трафик — при подключении система попросит пароль администратора",
    "s.bypass_ru": "Российские сайты (.ru, .su, .рф) открывать напрямую",
    "s.auto_reconnect": "Переподключаться, если VPN перестал работать",
    "s.auto_reconnect_hint": "Клиент проверяет соединение каждые 20 секунд. Если сервер перестал отвечать, Vlessia переподключится, а если не поможет — перейдёт на самый быстрый сервер той же подписки.",
    "s.kill_switch": "Kill switch: блокировать интернет, когда VPN выключен",
    "s.kill_hint": "Адаптер TUN остаётся включённым, а весь трафик уходит в пустоту: интернет пропадает, утечек нет. В «Весь трафик» без прав администратора не включается.",
    "s.kill_hint_android": "VPN-интерфейс остаётся включённым, поэтому Android сам блокирует весь трафик, пока VPN не подключится.",
    "s.balance": "Балансировка между серверами подписки",
    "s.balance_hint": "Новые соединения распределяются между серверами одной подписки (round-robin). Не работает для WireGuard, AmneziaWG и OpenVPN.",
    "s.more": "Дополнительно",
    "s.dns": "DNS-серверы",
    "s.dns_hint": "Через запятую. Первый также назначается адаптеру в режиме «Весь трафик».",
    "s.dns_hint_android": "Через запятую. Первый также назначается VPN-подключению Android.",
    "s.socks": "Порт SOCKS5",
    "s.http": "Порт HTTP",
    "s.startup": "Запуск",
    "s.autostart": "Запускать вместе с системой",
    "s.autostart_h_dock": "Клиент будет запускаться при входе в систему свёрнутым в Dock.",
    "s.autostart_h_tray": "Клиент будет запускаться при входе в систему сразу в трей.",
    "s.autostart_h_admin_off": "Клиент будет запускаться сразу в трей с правами администратора — без окна UAC, все режимы работают.",
    "s.autostart_h_no_admin": "Клиент запущен без прав администратора, поэтому при автозапуске будет работать только режим «Прокси». Чтобы работал «Весь трафик», запустите Vlessia от имени администратора и включите автозапуск.",
    "s.autostart_h_elevated": "Запускается сразу в трей с правами администратора, все режимы работают.",
    "s.autostart_h_degraded": "Запускается без прав администратора: «Весь трафик», WireGuard, AmneziaWG и OpenVPN не подключатся сами. Запустите Vlessia от имени администратора и включите автозапуск заново.",
    "s.autostart_h_android": "Чтобы VPN включался сам после перезагрузки телефона, включите для Vlessia «Постоянный VPN» в настройках Android.",
    "s.vpn_settings_btn": "Открыть настройки VPN",
    "s.auto_connect": "Подключаться к выбранному серверу при запуске",
    "s.auto_connect_android": " Подключаться к выбранному серверу при открытии приложения",
    "s.sound": "Звук при подключении",
    "s.rules": "Правила для сайтов",
    "s.rules_vpn": "Всегда через VPN",
    "s.rules_direct": "Всегда напрямую",
    "s.rules_hint": "По одному на строку: домен (вместе с поддоменами), IP-адрес или подсеть. «Через VPN» важнее «Напрямую» и галочки про российские сайты.",
    "s.appearance": "Оформление",
    "s.language": "Язык",
    "s.theme": "Тема",
    "theme.system": "Как в системе",
    "theme.light": "Светлая",
    "theme.dark": "Тёмная",
    "s.accent": "Цвет акцента",
    "accent.lagoon": "Лагуна",
    "accent.blue": "Синий",
    "accent.violet": "Фиолетовый",
    "accent.rose": "Розовый",
    "accent.orange": "Оранжевый",
    "accent.green": "Зелёный",
    "accent.graphite": "Графит",
    "s.animations": "Анимации",
    "s.updates": "Обновления",
    "s.update_check": "Проверять обновления автоматически",
    "s.check_now": "Проверить сейчас",
    "s.log": "Журнал",
    "s.log_level": "Подробность журнала Xray",
    "log.error": "Только ошибки",
    "log.warning": "Предупреждения",
    "log.info": "Подробно",
    "log.debug": "Отладка",
    "log.none": "Не вести",
    "s.open_dir": "Открыть папку данных",
    "logs.auto": "Обновлять автоматически",
    "logs.refresh": "Обновить",
    "d.add_title": "Добавить серверы",
    "d.add_sub": "Вставьте ссылки (по одной на строку), URL подписки или содержимое файла .conf / .ovpn. Файл может быть и картинкой с QR-кодом.",
    "d.add_aria": "Ссылки",
    "d.add_file": "Открыть файл…",
    "d.cancel": "Отмена",
    "d.add": "Добавить",
    "d.admin_title": "Нужны права администратора",
    "d.admin_sub": "Режим «Весь трафик» создаёт виртуальный сетевой адаптер — Windows разрешает это только администратору. WireGuard, AmneziaWG и OpenVPN работают только в этом режиме.",
    "d.admin_proxy": "Подключить через прокси",
    "d.admin_restart": "Перезапустить от администратора",
    "d.auth_title": "Вход OpenVPN",
    "d.auth_pre": "Сервер",
    "d.auth_post": "просит логин и пароль. Они сохранятся в настройках клиента.",
    "d.auth_user_ph": "Логин",
    "d.auth_pass_ph": "Пароль",
    "d.login": "Войти",
    "d.rename_title": "Название подписки",
    "d.rename_sub": "Оставьте пустым, чтобы показывать название от провайдера.",
    "d.rename_ph": "Название",
    "d.save": "Сохранить",
    "d.close": "Закрыть",
    "d.notes_title": "Что нового",
    "d.notes_none": "Описание изменений не опубликовано.",
    "d.notes_version": "Что нового в версии {0}",
    "d.welcome_title": "Бесплатный VPN навсегда",
    "d.welcome_sub": "Нет своего сервера? Получите бесплатный VPN навсегда в Telegram-боте @v4hood_bot, а затем добавьте его в Vlessia кнопкой «Вставить».",
    "d.welcome_no": "Не сейчас",
    "d.welcome_bot": "Открыть бота",
    "d.via_title": "Цепочка: второй сервер",
    "d.via_relay": "Ретранслятор",
    "d.via_sub": "Подключение пойдёт через выбранный сервер: {0} → {1} → интернет.",
    "d.via_none": "Без цепочки",
    "upd.download": "Загружаю версию {0}…",
    "upd.install": "Устанавливаю версию {0}, клиент перезапустится",
    "upd.available": "Доступна версия {0}",
    "upd.checking": "Проверяю…",
    "upd.latest": "Это последняя версия.",
    "upd.dl": "Загружаю обновление…",
    "upd.inst": "Устанавливаю обновление…",
    "upd.installed": "Установлена версия {0}.",
  },

  en: {
    "status.connected": "Connected",
    "status.connecting": "Connecting…",
    "status.disconnecting": "Disconnecting…",
    "status.disconnected": "Disconnected",
    "dock.no_server": "No server selected",
    "dock.send": "Upload",
    "dock.receive": "Download",
    "dock.mode_aria": "Mode",
    "dock.proxy": "Proxy",
    "dock.tun": "All traffic",
    "dock.connect": "Connect",
    "dock.disconnect": "Disconnect",
    "dock.test": "Test connection",
    "dock.testing": "Testing…",
    "dock.update": "Update",
    "dock.download": "Download",
    "dock.whats_new": "What's new",
    "dock.detail_tun": "All traffic goes through the VPN",
    "dock.detail_proxy": "Proxy 127.0.0.1:{0}",
    "dock.detail_sub": "Subscription: {0}",
    "dock.detail_via": "via {0}",
    "dock.detail_balance": "balancing: {0} {1}",
    "dock.err_need_admin": "The All traffic mode needs administrator rights",
    "dock.err_need_auth": "Enter the OpenVPN login and password",
    "dock.kill": "Kill switch: the internet is blocked",
    "dock.tun_locked": "WireGuard, AmneziaWG and OpenVPN work only in All traffic mode",
    "dock.version": "Version {0}",
    "dock.version_dev": "Development version",
    "dock.admin": "administrator rights",
    "dock.session": "Session: {0}, today: {1}",
    "dock.today": "Today through the VPN: {0}",
    "tab.servers": "Servers",
    "tab.subs": "Subscriptions",
    "tab.settings": "Settings",
    "tab.logs": "Logs",
    "servers.search_ph": "Find a server",
    "servers.ping_title": "Check the servers' ping",
    "servers.update_subs_title": "Update all subscriptions",
    "servers.qr_title": "Find a server or subscription QR code on screen",
    "servers.qr_android": "Scan a server or subscription QR code",
    "servers.qr_aria": "Add by QR code",
    "servers.add": "Add",
    "servers.paste": "Paste",
    "servers.paste_title": "Add a link or subscription from the clipboard",
    "servers.empty": "Add your first server",
    "servers.empty_sub": "Copy a server or subscription link from your provider and press Paste.",
    "servers.free_bot": "Free VPN at @v4hood_bot",
    "servers.manual": "Manual",
    "servers.not_found": "Nothing found",
    "servers.not_found_sub": "Try another name or address.",
    "servers.login": "Login",
    "servers.copy": "Copy",
    "servers.del": "Delete",
    "servers.via": "Via",
    "servers.via_set": "via {0}",
    "ping.none": "no response",
    "ping.ms": "{0} ms",
    "ping.na_title": "This protocol does not answer the check — try connecting",
    "sub.fallback": "Subscription",
    "sub.left": "left: {0}",
    "sub.unlimited": "unlimited",
    "sub.expired": "expired",
    "unit.day": ["day", "days"],
    "unit.server": ["server", "servers"],
    "unit.server_added": ["server added", "servers added"],
    "auto.title": "Fastest server",
    "auto.on": "On",
    "auto.now": "Now: {0}",
    "auto.meta_sub": "By ping, among the servers of the subscription “{0}”",
    "auto.meta_manual": "By ping, among the manual servers",
    "toast.copied_link": "Link copied",
    "toast.copied_conf": "Configuration copied",
    "toast.server_deleted": "Server deleted",
    "toast.added": "{0} {1}",
    "toast.auto_on_conn": "Reconnecting to the fastest server",
    "toast.auto_on": "I will pick the fastest server before connecting",
    "toast.login_saved": "Login saved",
    "toast.test_ok": "Connection works, {0} ms",
    "toast.no_subs": "No subscriptions yet — add them in the Subscriptions tab",
    "toast.subs_updated": "Subscriptions updated",
    "toast.saved": "Saved",
    "toast.via_set": "Chain: via {0}",
    "toast.via_off": "Chain disabled",
    "subs.url_ph": "Subscription URL, https://…",
    "subs.url_aria": "Subscription URL",
    "subs.name_ph": "Name (optional)",
    "subs.name_aria": "Name",
    "subs.add": "Add subscription",
    "subs.empty": "No subscriptions yet",
    "subs.empty_sub": "Paste a subscription link from your provider — the servers and the traffic left update automatically.",
    "subs.site": "Site",
    "subs.support": "Support",
    "subs.update": "Update",
    "subs.rename": "Rename",
    "subs.traffic_none": "The provider does not report the traffic usage.",
    "subs.traffic_unl": "Unlimited traffic",
    "subs.used": "used: {0}",
    "subs.left": "Left: {0}",
    "subs.of": "of {0}",
    "subs.forever": "No expiry",
    "subs.expired_on": "Expired on {0}",
    "subs.valid": "Valid until {0}, {1} {2} left",
    "subs.foot": "{0} {1}, updated {2}, auto-update every {3} h",
    "subs.never": "never updated",
    "subs.err": "Update failed: {0}",
    "toast.sub_added": "Subscription added: {0} {1}",
    "toast.sub_updated": "Subscription updated: {0} {1}",
    "toast.sub_deleted": "Subscription deleted",
    "s.connection": "Connection",
    "s.mode": "Mode",
    "s.mode_proxy": "Proxy — browsers and most apps",
    "s.mode_tun": "All traffic — needs administrator rights",
    "s.mode_tun_unix": "All traffic — the system asks for the administrator password on connect",
    "s.bypass_ru": "Open Russian sites (.ru, .su) directly",
    "s.auto_reconnect": "Reconnect when the VPN stops working",
    "s.auto_reconnect_hint": "The client checks the connection every 20 seconds. If the server stops answering, Vlessia reconnects, and if that does not help it switches to the fastest server of the same subscription.",
    "s.kill_switch": "Kill switch: block the internet when the VPN is off",
    "s.kill_hint": "The TUN adapter stays up and all traffic goes into a black hole: the internet disappears, nothing leaks. It cannot be enabled without administrator rights in All traffic mode.",
    "s.kill_hint_android": "The VPN interface stays up, so Android blocks all the traffic itself until the VPN connects.",
    "s.balance": "Balance across the subscription's servers",
    "s.balance_hint": "New connections are spread across the servers of one subscription (round-robin). Does not work for WireGuard, AmneziaWG and OpenVPN.",
    "s.more": "Advanced",
    "s.dns": "DNS servers",
    "s.dns_hint": "Comma separated. The first one is also set on the adapter in All traffic mode.",
    "s.dns_hint_android": "Comma separated. The first one is also set on the Android VPN.",
    "s.socks": "SOCKS5 port",
    "s.http": "HTTP port",
    "s.startup": "Startup",
    "s.autostart": "Start with the system",
    "s.autostart_h_dock": "The client starts minimized to the Dock at log in.",
    "s.autostart_h_tray": "The client starts straight to the tray at log in.",
    "s.autostart_h_admin_off": "The client starts straight to the tray with administrator rights — no UAC window, all modes work.",
    "s.autostart_h_no_admin": "The client runs without administrator rights, so at log in only the Proxy mode works. To enable All traffic, run Vlessia as an administrator and turn on autostart.",
    "s.autostart_h_elevated": "Starts straight to the tray with administrator rights, all modes work.",
    "s.autostart_h_degraded": "Starts without administrator rights: All traffic, WireGuard, AmneziaWG and OpenVPN will not connect by themselves. Run Vlessia as an administrator and turn on autostart again.",
    "s.autostart_h_android": "To have the VPN start by itself after a phone reboot, enable Always-on VPN for Vlessia in Android settings.",
    "s.vpn_settings_btn": "Open VPN settings",
    "s.auto_connect": "Connect to the selected server on start",
    "s.auto_connect_android": " Connect to the selected server when the app opens",
    "s.sound": "Sound on connect",
    "s.rules": "Site rules",
    "s.rules_vpn": "Always through VPN",
    "s.rules_direct": "Always directly",
    "s.rules_hint": "One per line: a domain (with subdomains), an IP or a network. Through VPN beats Direct and the Russian sites checkbox.",
    "s.appearance": "Appearance",
    "s.language": "Language",
    "s.theme": "Theme",
    "theme.system": "Follow the system",
    "theme.light": "Light",
    "theme.dark": "Dark",
    "s.accent": "Accent colour",
    "accent.lagoon": "Lagoon",
    "accent.blue": "Blue",
    "accent.violet": "Violet",
    "accent.rose": "Rose",
    "accent.orange": "Orange",
    "accent.green": "Green",
    "accent.graphite": "Graphite",
    "s.animations": "Animations",
    "s.updates": "Updates",
    "s.update_check": "Check for updates automatically",
    "s.check_now": "Check now",
    "s.log": "Log",
    "s.log_level": "Xray log detail",
    "log.error": "Errors only",
    "log.warning": "Warnings",
    "log.info": "Verbose",
    "log.debug": "Debug",
    "log.none": "Off",
    "s.open_dir": "Open the data folder",
    "logs.auto": "Refresh automatically",
    "logs.refresh": "Refresh",
    "d.add_title": "Add servers",
    "d.add_sub": "Paste links (one per line), a subscription URL or the content of a .conf / .ovpn file. The file may be a picture of a QR code.",
    "d.add_aria": "Links",
    "d.add_file": "Open a file…",
    "d.cancel": "Cancel",
    "d.add": "Add",
    "d.admin_title": "Administrator rights required",
    "d.admin_sub": "The All traffic mode creates a virtual network adapter — Windows allows that only for administrators. WireGuard, AmneziaWG and OpenVPN work only in this mode.",
    "d.admin_proxy": "Connect through the proxy",
    "d.admin_restart": "Restart as an administrator",
    "d.auth_title": "OpenVPN login",
    "d.auth_pre": "Server",
    "d.auth_post": "asks for a login and a password. They will be stored in the client settings.",
    "d.auth_user_ph": "Login",
    "d.auth_pass_ph": "Password",
    "d.login": "Log in",
    "d.rename_title": "Subscription name",
    "d.rename_sub": "Leave it empty to show the provider's name.",
    "d.rename_ph": "Name",
    "d.save": "Save",
    "d.close": "Close",
    "d.notes_title": "What's new",
    "d.notes_none": "No release notes published.",
    "d.notes_version": "What's new in version {0}",
    "d.welcome_title": "Free VPN for life",
    "d.welcome_sub": "No server of your own? Get a free VPN for life from the Telegram bot @v4hood_bot, then add it to Vlessia with the Paste button.",
    "d.welcome_no": "Not now",
    "d.welcome_bot": "Open the bot",
    "d.via_title": "Chain: the second server",
    "d.via_relay": "Relay",
    "d.via_sub": "The connection goes through the selected server: {0} → {1} → the internet.",
    "d.via_none": "No chain",
    "upd.download": "Downloading version {0}…",
    "upd.install": "Installing version {0}, the client will restart",
    "upd.available": "Version {0} is available",
    "upd.checking": "Checking…",
    "upd.latest": "This is the latest version.",
    "upd.dl": "Downloading the update…",
    "upd.inst": "Installing the update…",
    "upd.installed": "Version {0} installed.",
  },
};

// Exact Go messages (no dynamic parts).
const GO_EXACT = {
  ru: {},
  en: {
    "выберите сервер": "Select a server",
    "сервер не найден": "Server not found",
    "сервер для цепочки не найден": "The chain server is not found",
    "неизвестный режим": "Unknown mode",
    "неверные порты": "Invalid ports",
    "файл слишком большой": "The file is too large",
    "пусто": "Empty",
    "ссылки не найдены": "No links found",
    "неверный URL подписки": "Invalid subscription URL",
    "подписка не найдена": "Subscription not found",
    "подписка не вернула ни одного сервера": "The subscription returned no servers",
    "не подключено": "Not connected",
    "Соединение через VPN пропало — переподключаюсь": "The VPN connection is lost — reconnecting",
    "Kill switch работает только в режиме «Весь трафик», для него нужны права администратора":
      "The kill switch works only in All traffic mode and needs administrator rights",
    "Kill switch: блокировка интернета остановлена, VPN выключен":
      "Kill switch: the internet block was stopped by the system, the VPN is off",
    "не нужно на Android": "Not needed on Android",
  },
};

// Go messages with dynamic parts: [regexp, template]. {1}… are the capture
// groups; unknown inner Go errors are translated recursively.
const GO_TEMPLATES = {
  ru: [],
  en: [
    [/^загрузка подписки: (.+)$/, "Loading the subscription: {1}"],
    [/^в подписке нет рабочих ссылок: (.+)$/, "No working links in the subscription: {1}"],
    [/^не удалось найти IP сервера (.+): (.+)$/, "Cannot resolve the server IP {1}: {2}"],
    [/^не удалось найти IP реле (.+): (.+)$/, "Cannot resolve the relay IP {1}: {2}"],
    [/^Hysteria: сервер не отвечает: (.+)$/, "Hysteria: the server does not answer: {1}"],
    [/^Hysteria: релей не отвечает: (.+)$/, "Hysteria: the relay does not answer: {1}"],
    [/^не удалось включить системный прокси: (.+)$/, "Cannot enable the system proxy: {1}"],
    [/^туннель (\S+) не запущен$/, "The {1} tunnel is not running"],
    [/^протокол "(.*)" не поддерживается$/, "Protocol “{1}” is not supported"],
    [/^AmneziaWG: (.+)$/, "AmneziaWG: {1}"],
    [/^OpenVPN: (.+)$/, "OpenVPN: {1}"],
    [/^WireGuard: (.+)$/, "WireGuard: {1}"],
    [/^Outline: (.+)$/, "Outline: {1}"],
    [/^QR-код на экране не найден\. (.+)$/, "No QR code found on screen. {1}"],
    [/^перезапуск отменён: (.+)$/, "Restart cancelled: {1}"],
    [/^Сервер «(.+)» не отвечает — переключаюсь на «(.+)»$/, "Server “{1}” does not answer — switching to “{2}”"],
    [/^Подписка «(.+)»: (.+)$/, "Subscription “{1}”: {2}"],
    [/^не удалось проверить обновления: (.+)$/, "Cannot check for updates: {1}"],
    [/^Не удалось проверить обновления: (.+)$/, "Cannot check for updates: {1}"],
    [/^Не удалось скачать обновление: (.+)$/, "Cannot download the update: {1}"],
    [/^ошибка конфига xray: (.+)$/, "Xray config error: {1}"],
    [/^не удалось создать xray: (.+)$/, "Cannot start Xray: {1}"],
    [/^помощник TUN: (.+)$/, "TUN helper: {1}"],
    [/^помощник TUN завершился, не подключившись$/, "The TUN helper exited without connecting"],
    [/^истекло время ожидания пароля$/, "The password prompt timed out"],
    [/^помощник TUN не прошёл проверку$/, "The TUN helper failed the check"],
    [/^запрос пароля отменён$/, "The password prompt was cancelled"],
    [/^для режима «Весь трафик» нужен pkexec \(пакет polkit\)$/, "All traffic mode needs pkexec (the polkit package)"],
    [/^адаптер (\S+) не появился$/, "The adapter {1} did not appear"],
    [/^интерфейс (\S+) не появился$/, "The interface {1} did not appear"],
    [/^netsh (.+): (.+)$/, "netsh {1}: {2}"],
    [/^не удалось сделать снимок экрана/, "Cannot take a screenshot"],
  ],
};

let lang = "ru";
try {
  const saved = localStorage.getItem("vlessia-lang");
  if (saved === "en" || saved === "ru") lang = saved;
} catch { /* private mode */ }

let langChanged = null; // app.js registers a callback (re-render, tray)

function setLang(l, notify) {
  lang = l === "en" ? "en" : "ru";
  try { localStorage.setItem("vlessia-lang", lang); } catch { /* private mode */ }
  document.documentElement.lang = lang;
  applyLang();
  if (notify && langChanged) langChanged(lang);
}

// t translates a key with {0}, {1}… placeholders. Plural groups (arrays)
// are returned as is; use unitPlural for them.
function t(key, ...args) {
  let s = (I18N[lang] && I18N[lang][key]) ?? I18N.ru[key] ?? key;
  if (Array.isArray(s) || !args.length) return s;
  args.forEach((a, i) => { s = s.split("{" + i + "}").join(String(a)); });
  return s;
}

// plural picks a noun form: Russian needs one/few/many, English two.
function plural(n, ...forms) {
  if (lang === "en") return n === 1 ? forms[0] : (forms[1] ?? forms[0]);
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return forms[0];
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return forms[1];
  return forms[2];
}

// unitPlural picks from the unit.* arrays in the dictionary.
function unitPlural(unit, n) {
  const forms = t(unit);
  return Array.isArray(forms) ? plural(n, ...forms) : forms;
}

// applyLang localises the static markup.
function applyLang() {
  $$("[data-i18n]").forEach((el) => { el.textContent = t(el.dataset.i18n); });
  $$("[data-i18n-ph]").forEach((el) => { el.placeholder = t(el.dataset.i18nPh); });
  $$("[data-i18n-title]").forEach((el) => { el.title = t(el.dataset.i18nTitle); });
  $$("[data-i18n-aria]").forEach((el) => { el.setAttribute("aria-label", t(el.dataset.i18nAria)); });
}

// trGo translates a message coming from the Go core when the UI is English.
function trGo(s) {
  if (!s || lang === "ru") return s;
  const exact = GO_EXACT.en[s];
  if (exact !== undefined) return exact;
  for (const [re, tpl] of GO_TEMPLATES.en) {
    const m = String(s).match(re);
    if (m) {
      return tpl.replace(/\{(\d)\}/g, (_, i) => trGo(m[i] ?? ""));
    }
  }
  return s;
}

// ---------- formatters, language-aware ----------

function fmtSpeed(b) {
  if (lang === "en") {
    if (b < 1024) return b + " B/s";
    if (b < 1024 * 1024) return (b / 1024).toFixed(1) + " KB/s";
    return (b / 1024 / 1024).toFixed(1) + " MB/s";
  }
  if (b < 1024) return b + " Б/с";
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + " КБ/с";
  return (b / 1024 / 1024).toFixed(1) + " МБ/с";
}

function fmtBytes(b) {
  const u = lang === "en" ? ["B", "KB", "MB", "GB", "TB"] : ["Б", "КБ", "МБ", "ГБ", "ТБ"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return (i ? b.toFixed(1) : b) + " " + u[i];
}

function fmtDate(unix) {
  const loc = lang === "en" ? "en-US" : "ru-RU";
  return new Date(unix * 1000).toLocaleString(loc, { day: "numeric", month: "long", hour: "2-digit", minute: "2-digit" });
}

function fmtDateFull(unix) {
  const loc = lang === "en" ? "en-US" : "ru-RU";
  return new Date(unix * 1000).toLocaleDateString(loc, { day: "numeric", month: "long", year: "numeric" });
}
