# MinIO S3 Backend API

Go API для работы с S3-совместимыми хранилищами и администрирования MinIO.
Сервис управляет подключениями, выполняет запросы с учётными данными пользователя
и предоставляет HTTP API для файловых и административных операций.

В основе — официальные SDK: [minio-go/v7](https://github.com/minio/minio-go)
для S3, [madmin-go/v3](https://github.com/minio/madmin-go) для MinIO Admin API
и [minio/pkg/v3](https://github.com/minio/pkg) для IAM-политик.
Маршруты сопоставляет [chi](https://github.com/go-chi/chi), OpenAPI строится
и проверяется через [kin-openapi](https://github.com/getkin/kin-openapi).
Настройки ENV разбирает [env](https://github.com/caarlos0/env), частоту попыток
входа ограничивает [x/time/rate](https://pkg.go.dev/golang.org/x/time/rate).
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

Нужны Docker с Compose и Make. Команды выполняются из корня репозитория:

```sh
git clone https://github.com/owiks/golang-minio-s3-backend-api.git
cd golang-minio-s3-backend-api
cp .env.example .env
# При необходимости настройте доступ к хранилищу в .env.
make docker-build
make state-init  # один раз: ключ шифрования в отдельном volume
make docker-up OFFLINE=1
make health
```

По умолчанию API доступен на `127.0.0.1:8081`:

| Адрес | Назначение |
| --- | --- |
| [GET /health](http://127.0.0.1:8081/health) | Проверка процесса API: `{"ok":true}`. |
| [GET /ready](http://127.0.0.1:8081/ready) | Готовность хранилища API: `200` при доступной БД, `503` при ошибке. |
| [Swagger UI](http://127.0.0.1:8081/api/docs/) | Документация без внешнего CDN; выполнение запросов отключено. |
| [OpenAPI JSON](http://127.0.0.1:8081/api/openapi.json) | Спецификация для клиентов и инструментов. |

`/health` проверяет процесс; `/ready` проверяет выбранную БД API с таймаутом
в одну секунду. Эти запросы не обращаются к пользовательским S3-серверам.
MinIO или другой S3-сервер подключается отдельно и не входит в production Compose.

```sh
make docker-ps
make docker-logs
make docker-down
```

[docker-compose.yml](docker-compose.yml) запускает API без root, с read-only
файловой системой и временным `/tmp`. SQLite хранится в `api_data`, ключ шифрования — отдельно в `api_keys`; runtime монтирует ключи только для чтения. Порт публикуется на loopback;
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
После изменения окружения пересоздайте контейнер через `make docker-up OFFLINE=1`.
Команда сначала собирает образ, останавливает API, выполняет отдельный
контейнер миграций и запускает API с проверкой healthcheck. `OFFLINE=1` явно
подтверждает, что остальные API-процессы, использующие эту БД, уже остановлены:
Make проверяет только собственный Compose-проект.

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
| `API_TOKEN_TTL` | `1h` | Срок Bearer-токена по умолчанию и верхний предел `ttlSeconds`: от `1s` до `24h`, целое число секунд. Cookie-сессии по-прежнему живут час. |
| `API_DB_DRIVER` | `memory` без ENV; `sqlite` в env-примере | Адаптер `memory`, `sqlite`, `postgres` или `mysql`. |
| `API_DB_HOST` | `127.0.0.1` | Хост PostgreSQL или MySQL, без схемы и порта. |
| `API_DB_PORT` | `5432` / `3306` | Порт PostgreSQL / MySQL; можно задать свой. |
| `API_DB_NAME` | пусто | Имя существующей БД. Обязательно для PostgreSQL/MySQL. |
| `API_DB_USER` | пусто | Логин пользователя БД. |
| `API_DB_PASSWORD` | пусто | Пароль пользователя БД; передаётся без дополнительного кодирования. |
| `API_DB_SQLITE_PATH` | пусто; `/var/lib/api/state.db` в Docker | Обычный путь к файлу SQLite. |
| `API_DB_TLS_MODE` | `verify-full` | Проверка сертификата и имени сервера; `disable` отключает TLS для локального окружения. |
| `API_DB_TLS_CA_FILE` | пусто | PEM-файл доверенного CA для внешней БД; иначе системные CA. |
| `API_STATE_KEYRING_FILE` | пусто | JSON keyring с внешними AES-256 ключами; обязателен для SQL. |
| `API_DB_MAX_OPEN` / `API_DB_MAX_IDLE` | `10` / `2` | Пул SQL-соединений, от 1 до 100; idle ≤ open. У SQLite одно соединение и WAL. |
| `API_DB_CONN_LIFETIME` | `30m` | Время жизни SQL-соединения, от `1s` до `24h`. |
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

Для запросов сессии нужна cookie или Bearer-токен; одного ID в URL недостаточно.
Вход через `POST /api/sessions` требует `Origin`. В режиме cookie все методы,
кроме GET, требуют `Origin` и `X-CSRF-Token`. Bearer-запросы их не требуют.
Параметры остальных операций описаны в OpenAPI.

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

### Bearer: запросы без cookie

`POST /api/tokens` принимает те же ключи подключения и необязательное
`ttlSeconds`. Без этого поля токен действует `API_TOKEN_TTL` (по умолчанию час).
Можно запросить меньший срок; нулевой, отрицательный или превышающий лимит
возвращает `400 / InvalidTokenTTL`.

```json
{
  "id": "SESSION_ID",
  "token": "ACCESS_TOKEN",
  "tokenType": "Bearer",
  "connectionID": "CONNECTION_ID",
  "revision": 1,
  "expiresAt": "2026-10-21T12:00:00Z",
  "expiresIn": 900
}
```

Токен возвращается один раз, cookie не устанавливается. Передавайте его только
в `Authorization: Bearer …`, используя `id` в обычных адресах API.
Пример: выдача на 15 минут, чтение списка бакетов, загрузка файла и отзыв.
Используется тот же `connection.json`, что в примере выше.

```sh
#!/bin/sh
set -eu
umask 077

base=http://127.0.0.1:8081
connection_file=/absolute/path/to/connection.json
upload_file=/absolute/path/to/example.txt
bucket=my-bucket
key=example.txt
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

jq '. + {ttlSeconds: 900}' "$connection_file" > "$workdir/request.json"
curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary "@$workdir/request.json" \
  "$base/api/tokens" > "$workdir/token.json"

session_id=$(jq -er '.id' "$workdir/token.json")
# Заголовок читается из закрытого файла и не попадает в аргументы процесса curl.
jq -er '"Authorization: Bearer " + .token' "$workdir/token.json" > "$workdir/auth-header"

curl --fail-with-body --silent --show-error \
  -H "@$workdir/auth-header" \
  "$base/api/sessions/$session_id/admin/buckets" | jq .

# absent означает «создать, только если файла ещё нет»; совпадение даст 409.
query=$(jq -nr --arg bucket "$bucket" --arg key "$key" \
  '"bucket=" + ($bucket|@uri) + "&key=" + ($key|@uri)')
curl --fail-with-body --silent --show-error \
  -H "@$workdir/auth-header" -H 'Content-Type: application/octet-stream' \
  -H 'X-Object-Revision: absent' --upload-file "$upload_file" \
  "$base/api/sessions/$session_id/objects/upload?$query" | jq .

# Продлите действующий токен до истечения срока. ID и секрет сохраняются.
jq '{revision,ttlSeconds:900}' "$workdir/token.json" > "$workdir/renew.json"
curl --fail-with-body --silent --show-error \
  -H "@$workdir/auth-header" -H 'Content-Type: application/json' \
  --data-binary "@$workdir/renew.json" \
  "$base/api/sessions/$session_id/tokens/$session_id/renew" | jq .

curl --fail-with-body --silent --show-error \
  -H "@$workdir/auth-header" \
  -X DELETE "$base/api/sessions/$session_id" | jq .
```

Для сетевого доступа используйте HTTPS; HTTP в примере предназначен для localhost.
У выдачи токена `Origin` необязателен. Если он передан, сервер проверяет его,
как при входе в веб. Ограничения попыток входа общие для обоих способов.

- Токен предоставляет права переданных ключей S3/MinIO. Он не расширяет IAM-права
  и не продлевает срок исходных STS-учётных данных.
- Cookie и Bearer создают отдельные сессии. Неверный `Authorization` даёт `401`,
  даже если запрос содержит рабочую cookie. Cookie нельзя использовать как Bearer.
- `DELETE /api/sessions/{id}` отзывает токен этой сессии. Следующие запросы
  возвращают `401 / InvalidToken` с `WWW-Authenticate: Bearer …`.
  Его дочерние токены тоже отзываются. Уже выполняющиеся запросы могут завершиться;
  независимые доступы сохраняются.
- В SQL хранятся SHA-256 случайных Bearer/cookie/CSRF секретов. Ключи S3 и
  Session Token зашифрованы AES-256-GCM; ключ шифрования находится вне БД.
- В SQL-режимах доступ сохраняется между перезапусками до исходного `expiresAt`.
  `memory` остаётся временным режимом. Перезапуск не продлевает срок.
- Ротация сохраняет ID и срок; `renew` отдельно меняет срок действующего токена.
  Истёкший или отозванный токен не восстанавливается. Секрет не передаётся в URL.

### Постоянное хранение подключений

Выбор БД не меняет HTTP-контракт. Репозитории используют `database/sql`:
`pgx/v5`, `go-sql-driver/mysql` и SQLite-драйвер `modernc.org/sqlite` без CGO.
SQL-миграции выполняет Goose; файлы для PostgreSQL, MySQL и SQLite встроены в
бинарник. API при запуске только проверяет совместимость схемы, без DDL.
Неприменённая/неизвестная версия или недоступный ключ шифрования останавливают запуск.

Для запуска бинарника напрямую:

```sh
make build
mkdir -p .secrets data
bin/api state keygen --out .secrets/keyring.json

# SQLite
export API_DB_DRIVER=sqlite
export API_DB_SQLITE_PATH=./data/state.db
export API_STATE_KEYRING_FILE="$PWD/.secrets/keyring.json"
bin/api migrate up
API_LISTEN=127.0.0.1:8081 bin/api
```

Для PostgreSQL или MySQL задайте отдельные поля подключения:

```dotenv
API_DB_DRIVER=postgres
API_DB_HOST=db.example.com
API_DB_PORT=5432
API_DB_NAME=minio_api
API_DB_USER=minio_api
API_DB_PASSWORD=YOUR_PASSWORD
API_DB_TLS_MODE=verify-full
```

Для MySQL: `API_DB_DRIVER=mysql`, порт `3306`. Пустой `API_DB_PORT` выбирает
стандартный порт выбранной БД. Пароль передаётся как есть, включая `@`, `/`,
пробелы и другие символы; экранирование для строки подключения не требуется.
В `.env` используйте одинарные кавычки, если значение содержит `$` или `#`;
синтаксис кавычек определяется Compose. При запуске бинарника экспортируйте
переменные в окружение процесса: сам Go API `.env` не читает.
В Docker `API_DB_DRIVER` нужно указать явно; Compose проверяет его до запуска.

По умолчанию TLS проверяет сертификат и имя хоста БД. Для своего CA задайте
`API_DB_TLS_CA_FILE`; в Docker дополнительно смонтируйте PEM-файл для чтения.
Для одноразовых локальных БД можно явно задать `API_DB_TLS_MODE=disable`.
Production Compose не запускает внешнюю БД и не добавляет её автоматически.
Используйте выделенную БД; права пользователя API и мигратора можно разделить.

Ключ шифрования нельзя хранить в той же БД или коммитить. Keyring — JSON вида
`{"active":"key-1","keys":{"key-1":"BASE64_32_BYTES"}}`; создавайте его командой
`state keygen`. Потеря keyring делает сохранённые ключи S3 нечитаемыми.
Docker keyring находится в отдельном volume; `docker compose down --volumes`
удалит и БД, и ключи. Обычный `make docker-down` volumes сохраняет.

### Миграции схемы

Миграции лежат в `src/persistence/migrations/sql/{postgres,mysql,sqlite}`.
Формат Goose: `00001_auth_state.sql` с разделами `-- +goose Up` и
`-- +goose Down`. Применённый SQL-файл не редактируют: изменение добавляют
следующим номером для всех трёх диалектов. Историю ведёт `goose_db_version`.

```sh
bin/api migrate status          # JSON: версии, имена файлов, applied
bin/api migrate version         # текущая версия; 0 до первого применения
# Остановите API перед изменением схемы.
bin/api migrate up --offline
# Только осознанный откат одной последней миграции; Down может удалять данные.
bin/api migrate down --offline
```

Локальные Make-команды: `migrate-up`, `migrate-status`, `migrate-version`,
`migrate-down`. `make migrate-up` не подставляет `--offline`: при переходе со
старой схемы после остановки API укажите `OFFLINE=1`. Для отката требуется
`make migrate-down OFFLINE=1`. В Docker — `make docker-migrate OFFLINE=1`
после остановки API; команда откажет, если API собственного Compose-проекта ещё активен.
`make docker-up OFFLINE=1` сам останавливает свой API и включает миграции перед
запуском. Остальные процессы с доступом к этой БД нужно остановить самостоятельно;
флаг подтверждает это условие и не заменяет проверку окружения. Подключение берётся из обычных
`API_DB_*`; DSN, отдельный CLI Goose и доступ к ключам шифрования не нужны.
`state migrate` сохранён как совместимый псевдоним `migrate up`.

При переходе со старого `api_schema` используйте `migrate up --offline`.
Команда проверяет прежнюю версию, точный checksum и необходимые таблицы,
затем регистрирует начальную версию через Goose без повторного создания таблиц.
Подключения, ciphertext, хеши токенов/cookie/CSRF, сроки, отзыв и аудит сохраняются.
Незнакомая или неполная схема не принимается автоматически.

Изменения схемы PostgreSQL и SQLite выполняются в транзакциях миграций.
У MySQL DDL имеет неявные commit, поэтому SQL отмечен `NO TRANSACTION`.
Ошибка оставляет миграцию неприменённой; часть DDL MySQL при этом может остаться.
Проверьте `migrate status` и фактическую схему перед исправлением и повтором.
Автоматического принудительного изменения версии или отката после ошибки нет.
Мутации сериализуются блокировками: PostgreSQL advisory lock, MySQL `GET_LOCK` на отдельном соединении
и файловый lock рядом с SQLite. SQLite используйте на локальной
файловой системе; файл `.goose.lock` вручную не удаляйте при работающих командах.
`status`, `version`, `verify`, `backup` и запуск API схему не создают.
В режиме `memory` команды миграций ничего не меняют и возвращают версию `0`.
Для миграций SQL-пользователю нужны DDL-права; API может работать с отдельным
пользователем с правами чтения/изменения таблиц и чтения метаданных схемы.

### Управление подключениями и доступами

Все адреса ниже находятся под `/api/sessions/{id}` и работают с Bearer либо
cookie + Origin + CSRF. `GET credential` возвращает собственный `connectionID`,
ограничения, ревизию и срок без секретов.

| Метод и путь | Действие |
| --- | --- |
| `GET connections` | Подключения того же владельца. |
| `GET connections/{connectionID}` | Сведения и ревизия подключения. |
| `DELETE connections/{connectionID}` | Отзыв доступов подключения и их потомков, удаление зашифрованных ключей; тело `{"revision":1}`. |
| `PUT connections/{connectionID}/credentials` | Проверка и замена ключей того же upstream-пользователя с сохранением доступов. |
| `GET / POST connections/{connectionID}/tokens` | Список токенов без секретов / выдача ограниченного токена. |
| `GET tokens/{tokenID}` | Сведения о токене без секрета. |
| `PATCH tokens/{tokenID}` | Изменение имени и сужение прав; ограничения распространяются на потомков. |
| `POST tokens/{tokenID}/rotate` | Новый секрет с сохранением ID и `expiresAt`. |
| `POST tokens/{tokenID}/renew` | Отдельное изменение срока действующего токена. |
| `DELETE tokens/{tokenID}` | Отзыв токена и его потомков с проверкой `revision`. |
| `GET auth-events` | Аудит владельца: создание, изменение, ротация, продление и отзыв. |

Владелец определяется парой нормализованный S3 endpoint + upstream Access Key.
ID подключения не является правом доступа. Доступ к другому подключению
того же владельца и полному аудиту дополнительно проверяет текущие исходные
ключи через S3/MinIO. Повторное создание MinIO-пользователя с тем же именем
и другим секретом не даёт старому API-токену управлять новыми подключениями.
При замене сохранённых ключей проверяется новая пара.
Управление требует `access:manage`, кроме продления собственного Bearer-токена;
токен не может выдавать или перехватывать доступ шире собственного. Замена ключей,
удаление подключения и чтение полного аудита требуют полного API-доступа.
Токен со scope `access:manage` является делегированным менеджером: его можно
использовать для создания новых доступов в пределах его прав и срока.
У токенов, выданных через Bearer, сохраняется `parentID`. Отзыв родителя
каскадно отзывает потомков, сужение прав пересекает их scopes и ресурсы,
а сокращение срока уменьшает сроки потомков. При пустом пересечении доступ
отзывается. Изменения выполняются атомарно, в том числе между подключениями
того же владельца. Расширение срока родителя не продлевает потомков автоматически;
ротация секрета не меняет родство.

Bearer, созданный через `POST /api/tokens` с ключами S3, и сервисный токен,
выданный через cookie-сессию, — самостоятельные доступы без `parentID`.
Выход из cookie-сессии их не отзывает; удаление подключения отзывает.

Пример выдачи токена только для чтения `reports/` в одном бакете
(используйте действующий токен; пример выше завершает его отзывом):

```sh
connection_id='CONNECTION_ID'
jq -n '{name:"reports-reader",ttlSeconds:900,policy:{scopes:["objects:read","buckets:read"],resources:[{bucket:"reports-bucket",prefix:"reports/"}]}}' > "$workdir/limited-token.json"
curl --fail-with-body --silent --show-error \
  -H @"$workdir/auth-header" -H 'Content-Type: application/json' \
  --data-binary @"$workdir/limited-token.json" \
  "$base/api/sessions/$session_id/connections/$connection_id/tokens"
```

Ответ содержит новый `id`, одноразовый `token`, `revision`, `expiresAt` и `expiresIn`.
Сохраните секрет в менеджере секретов; API не умеет показать его повторно.
При выдаче через Bearer дочерний срок не превышает срок родителя и `API_TOKEN_TTL`.
Через cookie выдаётся самостоятельный сервисный токен со сроком до `API_TOKEN_TTL`,
независимым от оставшегося времени cookie-сессии.

Scopes: `objects:read`, `objects:write`, `objects:delete`, `buckets:read`,
`buckets:manage`, `iam:read`, `iam:manage`, `server:read`, `server:manage`,
`access:manage`. Требования маршрутов есть в OpenAPI (`x-api-scopes`).
Отсутствие `resources` означает все ресурсы в пределах прав исходных ключей.
`prefix` — буквальное начало ключа, без glob: `reports/` не включает `reports-old/`.
Для настроек бакета нужен доступ ко всему бакету (`prefix` пустой).
Изменение IAM и сервера нельзя совмещать с ограничением bucket/prefix.
Чтение административных сведений определяется отдельными IAM/server scopes.

При копировании проверяются источник и назначение; при перемещении право
удаления проверяется до копирования. ZIP, версии, multipart и подписанные URL
проверяют те же ограничения. API-ссылки и приглашения дополнительно проверяют
активность и текущие ограничения родителя. Прямой presigned URL проверяет S3:
отзыв API-токена **не отзывает уже выданную подпись**; её срок ограничен
оставшимся сроком API-доступа. Для немедленного прекращения таких ссылок нужно
отозвать исходные S3-ключи или изменить native IAM-политику.

Ротация: `POST tokens/{tokenID}/rotate` с
`{"revision":1,"graceSeconds":0}`. По умолчанию старый секрет перестаёт работать
сразу; `graceSeconds` допускает перекрытие до 60 секунд. Старый секрет во время
перекрытия не выполняет административные изменения, а выданные им подписи
ограничены концом перекрытия. Обычные файловые операции сохраняются. Из двух конкурентных запросов с одной
ревизией сохраняется один; после `409` перечитайте состояние. Если ответ с новым
секретом потерян, используйте другой действующий менеджер для новой ротации.

Продление: `POST tokens/{tokenID}/renew` с `{"revision":2,"ttlSeconds":1800}`.
Действующий Bearer с `renewable: true` может продлить себя без `access:manage`;
новый срок отсчитывается от текущего времени и ограничен `API_TOKEN_TTL`,
а у дочернего токена — также сроком родителя. Требуются текущий секрет и ревизия:
старый секрет во время перекрытия не продлевает доступ. ID, секрет и права сохраняются.
Менеджер с `access:manage` может продлевать другой токен в пределах собственных
прав и срока. Истёкший или отозванный доступ не восстанавливается.
Cookie-сессия не продлевается: после часа войдите заново. Refresh-токенов нет.

У ранее сохранённых токенов без этих полей `renewable: false`: исходные сроки
и действующие права сохраняются, но самостоятельного продления нет. Для этого
один раз выдайте новый сервисный токен; старое родство по аудит-логам не угадывается.

Замена ключей S3 сохраняет ID, секреты API-доступов, их сроки и ограничения.
Новая пара должна принадлежать тому же upstream-пользователю и пройти проверку
хранилища. Следующие запросы используют новые ключи; уже отправленные в S3
запросы могут завершиться со старыми. Пример с действующим полным доступом:

```sh
connection_id='CONNECTION_ID'
replacement_file=/absolute/path/to/new-s3-credentials.json
# Файл содержит accessKey, secretKey и при необходимости sessionToken.
curl --fail-with-body --silent --show-error \
  -H "@$workdir/auth-header" \
  "$base/api/sessions/$session_id/connections/$connection_id" > "$workdir/connection-state.json"
jq --slurpfile current "$workdir/connection-state.json" \
  '. + {revision:$current[0].revision}' "$replacement_file" > "$workdir/replace-credentials.json"
curl --fail-with-body --silent --show-error \
  -H "@$workdir/auth-header" -H 'Content-Type: application/json' \
  --data-binary "@$workdir/replace-credentials.json" \
  -X PUT "$base/api/sessions/$session_id/connections/$connection_id/credentials" | jq .
```

### Резервные копии, перенос БД и смена ключа

Команды выполняются тем же бинарником и используют текущие `API_DB_*` и keyring.

CLI использует Cobra. Справка доступна для каждой команды; флаги относятся
к конкретной подкоманде:

```sh
./bin/api --help
./bin/api state backup --help
./bin/api migrate down --help
```

Запуск без аргументов по-прежнему поднимает HTTP API.
В SQL-режиме резервная копия — согласованный JSON-снимок с зашифрованными
S3-ключами и хешами случайных секретов. Храните её и keyring раздельно.
Новый файл создаётся с `0600`; существующий backup не перезаписывается.

```sh
# Проверка структуры БД и расшифровки; запросы в S3 не выполняются.
bin/api state verify
bin/api state backup --out /safe/path/api.state-backup.json

# Остановите API. Восстановление только в пустую БД, с исходным keyring.
# Все доступы из backup будут отозваны, чтобы старая копия не оживила секреты.
# Сначала примените схему выбранной пустой БД.
bin/api migrate up
bin/api state restore --offline --in /safe/path/api.state-backup.json

# Перенос в пустую БД другого типа: оба API остановлены, keyring общий.
# Сохраняются ID, хеши, ограничения, сроки и статусы, включая отзыв.
export API_TARGET_DB_DRIVER=postgres
export API_TARGET_DB_HOST=new-db.example.com
export API_TARGET_DB_PORT=5432
export API_TARGET_DB_NAME=minio_api
export API_TARGET_DB_USER=minio_api
export API_TARGET_DB_PASSWORD='YOUR_PASSWORD'
bin/api migrate up --target
bin/api state transfer --offline
```

`API_TARGET_DB_*` использует те же поля, что `API_DB_*`, независимо от исходного
подключения. Для SQLite назначения задайте `API_TARGET_DB_DRIVER=sqlite` и
`API_TARGET_DB_SQLITE_PATH`. Пароль назначения читается из ENV, не из аргументов.
При необходимости задайте `API_TARGET_DB_TLS_MODE` и `API_TARGET_DB_TLS_CA_FILE`.

`--offline` подтверждает, что API-процессы остановлены; команда сама их не
останавливает. После переноса запускайте только целевую БД: копии не синхронизируются.
Backup ограничен 64 МиБ, каталог — 16384 подключения и 16384 доступа,
один владелец — 256 подключений и 4096 доступов, одновременно действующих
токенов на подключение — 256. Аудит хранит последние 10000 событий.
Для освобождения квот можно удалить записи с давно истёкшим абсолютным сроком:
`bin/api state prune --before 2026-01-01T00:00:00Z`. Действующие доступы не удаляются.

Смена AES-ключа с сохранением действующих токенов:

```sh
# Остановите API; сохраните приватную копию текущего keyring.
bin/api state key-add --file .secrets/keyring.json
bin/api state reencrypt
bin/api state verify
# Запустите API с обновлённым keyring.
```

`key-add` добавляет новый активный ключ и сохраняет старые для расшифровки.
`reencrypt` меняет только ciphertext и ревизию подключения, не ID/сроки токенов;
повторный запуск продолжает работу после ошибки. Старые ключи удаляйте только
после успешной проверки и с учётом ключей, нужных для старых резервных копий.
В Docker те же команды доступны через `docker compose run --rm state-tools ...`.

### Ошибки и длительные операции

```json
{"error":{"code":"SessionExpired","message":"Сессия истекла. Отключитесь и подключитесь заново."}}
```

- `401 / SessionExpired` требует нового cookie-подключения; `401 / InvalidToken` — нового Bearer-токена; `403 / AccessDenied` означает отказ в действии.
- `409` при изменившейся ревизии требует перечитать состояние и повторно принять решение о записи.
- Потоки используют `application/x-ndjson`. Читайте терминальное событие и результаты элементов:
  одного HTTP 200 недостаточно для подтверждения всей операции.
- При неопределённом результате команды сначала проверьте состояние хранилища.
  Изменяющие команды Admin API автоматически не повторяются.

Для диагностики используйте `X-Request-ID` из ответа и `make docker-logs`.
Request-логи содержат шаблон операции, статус, длительность и безопасную причину
ошибки, без ключей, cookie, Bearer-токенов, тел запросов и подписанных URL.

## Границы гарантий

Cookie-сессии живут час; срок Bearer задаётся через `API_TOKEN_TTL` и `ttlSeconds`.
При `postgres`, `mysql`, `sqlite` подключения и доступы сохраняются в БД.
SDK-клиенты восстанавливаются лениво после проверки текущей сетевой политики.
При недоступной БД защищённые запросы получают `503`; обхода проверки из кеша нет.
В `memory` подключения остаются временными. Перезапуск сбрасывает локальное
состояние операций, общих папок, приглашений и ZIP, даже при SQL-хранении авторизации.
Управляемые загрузки возобновляются только в живой сессии и процессе.
После аварии могут остаться незавершённые multipart в S3.
SQL-адаптеры согласуют ротацию и отзыв между процессами, но runtime операций,
лимиты, IAM-блокировки и ревизии остаются локальными. Для операций нужна
маршрутизация сессии на один экземпляр; полноценный кластерный runtime сюда не входит.

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

Для локальной работы нужны Go 1.26.10+ и Make. Линтер устанавливается в `.tools`
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
make state-fixtures-up
STATE_INTEGRATION=1 make test-integration
make fixtures-down
make state-fixtures-down
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
  persistence/     SQL-адаптеры, миграции, шифрование ключей S3
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
Защищённой операции нужен явный scope-контракт в `src/http/api/routes/scopes.go`.
После изменения контракта выполните `make docs` и `make check`.

В Git сохраняются исходники, тесты, lock-файлы, OpenAPI, конфигурация сборки
и env-примеры. Локальные env, credentials, бинарники, `.tools`, coverage
и настройки IDE исключены через `.gitignore` и `.dockerignore`.
