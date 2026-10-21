# MinIO S3 API

Go API для работы с S3-совместимыми хранилищами и администрирования MinIO.
Сервис управляет подключениями, выполняет запросы с учётными данными пользователя
и предоставляет HTTP API для файловых и административных операций.

В основе — официальные SDK: [minio-go/v7](https://github.com/minio/minio-go)
для S3, [madmin-go/v3](https://github.com/minio/madmin-go) для MinIO Admin API
и [minio/pkg/v3](https://github.com/minio/pkg) для IAM-политик.
Версии закреплены в [go.mod](go.mod) и [go.sum](go.sum).

## Возможности

- Объекты и папки: загрузка, скачивание, копирование, перемещение, удаление,
  поиск, метаданные, метки, версии, контрольные суммы и подписанные ссылки.
- Загрузки: управляемые multipart-операции, продолжение после обрыва,
  прямая передача частей в S3 и отмена незавершённых загрузок.
- Бакеты: Lifecycle, CORS, квоты, versioning, Object Lock, политики и доступ.
- MinIO: пользователи, группы, IAM-политики, ключи, временные доступы,
  конфигурация, состояние сервера, пулы, tiers, пакетные задания и диагностика.
- S3 Select для CSV/JSON и capabilities API для проверки доступных возможностей.

Поддержка зависит от сервера и прав подключения. Административные операции
требуют MinIO Admin API; одного совместимого S3 endpoint для них недостаточно.
Полный контракт доступен во встроенном OpenAPI.

## Запуск API в Docker

Нужны Docker с Compose и Make. Команды выполняются из корня этого репозитория:

```sh
cp .env.example .env
# При необходимости настройте доступ к хранилищу в .env.
make docker-up
make health
```

По умолчанию API доступен на `127.0.0.1:8081`:

| Адрес | Назначение |
| --- | --- |
| [GET /health](http://127.0.0.1:8081/health) | Проверка процесса API: `{"ok":true}`. |
| [Swagger UI](http://127.0.0.1:8081/api/docs/) | Документация без внешнего CDN; выполнение запросов отключено. |
| [OpenAPI JSON](http://127.0.0.1:8081/api/openapi.json) | Спецификация для клиентов и инструментов. |

Healthcheck не проверяет доступность хранилища. MinIO или другой S3-сервер
подключается отдельно и не входит в production Compose.

```sh
make docker-ps
make docker-logs
make docker-down
```

[docker-compose.yml](docker-compose.yml) запускает API без root, с read-only
файловой системой и временным `/tmp`. Порт публикуется на loopback;
`API_BIND` и `API_PORT` меняют адрес публикации. Внутри контейнера API слушает `:8081`.

## Подключение к хранилищу

Используйте адрес **S3 API**, например `https://s3.example.com`.
Для MinIO это обычно порт `9000`, а не порт Console `9001`.
Адрес должен содержать только схему, хост и необязательный порт.

Публичные HTTPS endpoints разрешены по умолчанию. Внутренние адреса и HTTP
нужно явно разрешить в `.env`:

```dotenv
MINIO_ALLOWED_ENDPOINTS=http://minio:9000,https://s3.internal.example
```

Имя хранилища должно разрешаться из контейнера API. Для MinIO на Docker-хосте
можно задать соответствие публичного и внутреннего адресов:

```dotenv
MINIO_ENDPOINT_OVERRIDES='{"http://localhost:9000":"http://host.docker.internal:9000"}'
```

В запросе подключения укажите `http://localhost:9000`. API обращается к хранилищу
по внутреннему адресу, а подписанные URL сохраняют публичный endpoint.
Клиенту, который использует эти URL, нужен доступ к публичному адресу.
После изменения окружения пересоздайте контейнер через `make docker-up`.

## Конфигурация

Образец настроек — [.env.example](.env.example). Compose читает `.env`;
при локальном `make run` переменные нужно передать процессу самостоятельно.
Access Key и Secret Key передаются при создании сессии, а не задаются общим
административным ключом сервиса.

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `API_BIND` | `127.0.0.1` | Адрес публикации Docker-порта. |
| `API_PORT` | `8081` | Порт на Docker-хосте. |
| `API_LISTEN` | `:8081` | Listener при запуске Go напрямую; для локальной разработки — `127.0.0.1:8081`. |
| `API_LANGUAGE` | `auto` | Язык сообщений API: `auto` учитывает `Accept-Language`, `en` и `ru` задают язык принудительно. Без подходящего заголовка используется английский. |
| `APP_ORIGIN` | пусто | Разрешённый origin клиента: схема, хост и порт. Без значения проверяется origin самого API. |
| `MINIO_ALLOWED_ENDPOINTS` | пусто | Разрешённые внутренние/HTTP endpoints через запятую. |
| `MINIO_ENDPOINT_OVERRIDES` | пусто | JSON-соответствие публичных endpoints внутренним адресам; разрешает перечисленные исходные endpoints. |
| `API_MAX_TRANSFERS` | `4` | Общий допуск передач, от 1 до 64. |
| `API_MAX_TRANSFERS_PER_SESSION` | `2` | Допуск передач сессии, от 1 до 32 и не больше общего. |
| `API_MAX_LOGINS` | `8` | Одновременные подключения, от 1 до 64. |
| `API_LOGIN_BURST` | `60` | Начальный бюджет попыток подключения с адреса, от 1 до 1000. |
| `API_LOGIN_REFILL` | `1s` | Интервал восстановления одной попытки, от `100ms` до `1m`. |
| `API_TEMPORARY_BYTES` | `2147483648` | Общий бюджет временных файлов: 2 ГиБ; допустимо 16 МиБ–64 ГиБ. |
| `API_TRUSTED_PROXIES` | пусто | CIDR доверенных непосредственных прокси для `X-Real-IP`. |
| `API_CONDITIONAL_DELETE_ENDPOINTS` | пусто | Endpoints с подтверждённой поддержкой атомарного DELETE If-Match. |
| `API_CONDITIONAL_COMPLETE_ENDPOINTS` | пусто | Endpoints с подтверждёнными If-Match/If-None-Match при CompleteMultipartUpload. Требуются managed/direct загрузкам. |

Настройки conditional writes не расширяют сетевой allowlist и не предоставляют
IAM-права. Включайте их после проверки соответствующих условий на своём сервере.
За reverse proxy задайте `APP_ORIGIN` и настройте перезапись forwarded-заголовков.
`API_TRUSTED_PROXIES` отвечает за определение адреса клиента для rate limit.

## HTTP API

При `API_LANGUAGE=auto` язык сообщений задаётся заголовком запроса:

```sh
curl -i -H 'Accept-Language: ru-RU, en;q=0.8' http://127.0.0.1:8081/api/sessions/expired/access
```

Без подходящего заголовка сообщения приходят на английском. Язык указан
в `Content-Language`; коды ошибок и содержимое файлов от него не зависят.

`POST /api/sessions` принимает `endpoint`, `accessKey`, `secretKey`, необязательные
`region` и `sessionToken`. Регион по умолчанию — `us-east-1`.
Ответ `201` содержит `id`, `csrf`, `expiresAt`, доступные бакеты и разделы;
сервер также устанавливает HttpOnly cookie сессии.

Дальнейшие запросы имеют префикс `/api/sessions/{id}`:

| Запрос | Назначение |
| --- | --- |
| `GET /admin/buckets` | Доступные бакеты. |
| `GET /admin/buckets/{name}/details` | Сведения и настройки бакета. |
| `GET /capabilities` | Поддержка возможностей в текущем контексте. |
| `GET /objects/attributes` | Атрибуты объекта, версии и части. |
| `PUT /objects/upload` | Загрузка с необязательной ожидаемой checksum. |
| `POST /objects/multipart` | Управляемая multipart-загрузка. |
| `POST /objects/select` | Потоковый S3 Select. |
| `DELETE /` | Завершение сессии. |

Для запросов сессии нужна cookie; одного ID в URL недостаточно.
Вход требует `Origin`. Все сессионные методы, кроме GET, дополнительно требуют
`Origin` и `X-CSRF-Token`. Параметры остальных операций описаны в OpenAPI.

### Пример curl: подключение, чтение, выход

Потребуются `curl` и `jq`. Создайте JSON-файл подключения вне репозитория,
заменив endpoint и ключи своими значениями:

```json
{
  "endpoint": "https://s3.example.com",
  "accessKey": "YOUR_ACCESS_KEY",
  "secretKey": "YOUR_SECRET_KEY",
  "region": "us-east-1"
}
```

Укажите путь к этому файлу в `connection_file`. Ключи читаются из файла,
cookie и ответ подключения сохраняются в закрытом временном каталоге.

```sh
#!/bin/sh
set -eu
umask 077

connection_file=/absolute/path/to/connection.json
chmod 600 "$connection_file"
base=http://127.0.0.1:8081
origin=$base
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

curl --fail-with-body --silent --show-error \
  --cookie-jar "$workdir/cookies" \
  -H "Origin: $origin" -H 'Content-Type: application/json' \
  --data-binary "@$connection_file" \
  "$base/api/sessions" > "$workdir/session.json"

session_id=$(jq -er '.id' "$workdir/session.json")
csrf=$(jq -er '.csrf' "$workdir/session.json")

curl --fail-with-body --silent --show-error \
  --cookie "$workdir/cookies" \
  "$base/api/sessions/$session_id/admin/buckets" | jq .

curl --fail-with-body --silent --show-error \
  --cookie "$workdir/cookies" \
  -H "Origin: $origin" -H "X-CSRF-Token: $csrf" \
  -X DELETE "$base/api/sessions/$session_id" | jq .
```

Если настроен `APP_ORIGIN`, используйте его значение в переменной `origin`.

### Ошибки и длительные операции

```json
{"error":{"code":"SessionExpired","message":"Сессия истекла. Отключитесь и подключитесь заново."}}
```

- `401 / SessionExpired` требует нового подключения; `403 / AccessDenied` означает отказ в действии.
- `409` при изменившейся ревизии требует перечитать состояние и повторно принять решение о записи.
- Потоки используют `application/x-ndjson`. Читайте терминальное событие и результаты элементов:
  одного HTTP 200 недостаточно для подтверждения всей операции.
- При неопределённом результате команды сначала проверьте состояние хранилища.
  Изменяющие команды Admin API автоматически не повторяются.

Для диагностики используйте `X-Request-ID` из ответа и `make docker-logs`.
Request-логи содержат шаблон операции, статус, длительность и безопасную причину
ошибки, без ключей, cookie, тел запросов и подписанных URL.

## Границы гарантий

Сессии живут до часа, учётные данные хранятся в памяти API.
Перезапуск сбрасывает сессии и локальное состояние операций.
Управляемые загрузки возобновляются только в живой сессии и процессе.
После аварии могут остаться незавершённые multipart в S3.
Для нескольких экземпляров API нужна маршрутизация сессии на один экземпляр.

Окончательную авторизацию выполняет хранилище. Capabilities различают
`supported`, `unsupported`, `access_denied`, `unknown`; отсутствие доказательства
не означает поддержку. Checksum/direct/CORS capabilities пока остаются `unknown`.

Записи используют ревизии и нативные условия там, где они доступны.
Перемещение — копирование с последующим удалением, без общей транзакции двух ключей.
Создание пользователей, групп и политик использует upsert и требует
`acknowledgeNonAtomicCreate: true`: проверка существования не закрывает внешнюю гонку.

ETag не считается MD5. Checksum receipt отдельно показывает проверку API и
подтверждение хранилищем. Обычная и управляемая загрузка ограничены 5 ГиБ;
предварительная проверка checksum также требует достаточного временного бюджета.
S3 Select зависит от поддержки сервера и не принимает VersionID, Range и сжатие.

## Разработка и проверки

Для локальной работы нужны Go 1.26 и Make. Линтер устанавливается в `.tools`
с закреплённой версией; зависимости приложения при этом не обновляются.

```sh
make deps
make tools
make check

# Локальный API; .env автоматически не загружается.
API_LISTEN=127.0.0.1:8081 make run
```

| Команда | Результат |
| --- | --- |
| `make help` | Все доступные команды. |
| `make check` | Формат, OpenAPI, lint с vet, race-тесты и сборка. |
| `make test` / `make test-race` | Тесты и вариант с race detector. |
| `make test-cover` | Тесты и `coverage/coverage.out`. |
| `make fmt` / `make fmt-check` | Форматирование и проверка без изменения файлов. |
| `make docs` / `make docs-check` | Генерация OpenAPI из маршрутов и проверка актуальности. |
| `make build` | Бинарник `bin/api`. |
| `make clean` | Удаление результатов сборки и coverage. |

По умолчанию тесты с настоящим MinIO пропускаются. Для них есть отдельный
[docker-compose.test.yml](docker-compose.test.yml): одноразовые серверы с тестовыми
ключами и tmpfs на портах `19000`, `19001`, `19002`.

```sh
make fixtures-up
make test-integration
make fixtures-down
```

Эти тесты изменяют данные и должны выполняться только на одноразовых fixtures.
Вывод пула из эксплуатации включается отдельно через `MINIO_POOL_DECOMMISSION=1`;
после него fixture пула нужно пересоздать перед другими проверками топологии.

## Структура проекта

```text
src/
  main.go          точка входа
  cmd/             запуск и сигналы завершения
  config/          загрузка конфигурации
  http/            маршруты, контроллеры, middleware, Swagger и потоки
  services/        сценарии, доменные модели, runtime и SDK-адаптеры
tests/
  architecture/    проверки границ пакетов
  integration/     HTTP-контракты и сценарии MinIO
  fixtures/        одноразовое тестовое окружение
tools/openapi/     генератор спецификации
```

Контроллер разбирает HTTP-вход и вызывает сервис сценария. Сервис получает узкие
интерфейсы зависимостей; runtime владеет состоянием, storage — SDK и транспортом.
Новый маршрут регистрируется в `src/http/api/catalog.go` через пакет раздела.
После изменения контракта выполните `make docs` и `make check`.

В Git сохраняются исходники, тесты, lock-файлы, OpenAPI, конфигурация сборки
и env-примеры. Локальные env, credentials, бинарники, `.tools`, coverage
и настройки IDE исключены через `.gitignore` и `.dockerignore`.
