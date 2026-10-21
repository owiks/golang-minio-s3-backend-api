# Расширения S3 API

Эти маршруты расширяют Go API и сохраняют прежние операции.
Зависимость `minio-go/v7` остаётся на `v7.4.1`: при проверке
21 октября 2026 года нового стабильного официального релиза не было.

Все пути ниже начинаются с `/api/sessions/{id}`. Нужна cookie подключения;
для PUT/POST/DELETE — также Origin и X-CSRF-Token. Подключение и curl-cookie jar
описаны в [README](README.md#пример-curl-подключение-чтение-выход).
OpenAPI генерируется из реестра маршрутов, а не из этого документа.

## Маршруты

| Метод и путь | Назначение |
| --- | --- |
| `GET /capabilities` | Поддержка конкретных операций в текущем контексте. |
| `GET /objects/attributes` | Атрибуты объекта и одна страница multipart-частей. |
| `PUT /objects/upload` | Прежняя загрузка с необязательной ожидаемой checksum. |
| `POST /objects/multipart` | Создание управляемой загрузки. |
| `GET /objects/multipart/{operation}` | Состояние и подтверждённые S3 части. |
| `PUT /objects/multipart/{operation}/parts/{part}` | Передача части через API. |
| `POST /objects/multipart/{operation}/parts/{part}/sign` | Подпись прямой передачи части. |
| `POST /objects/multipart/{operation}/complete` | Проверка и условное завершение. |
| `DELETE /objects/multipart/{operation}` | Отмена своей загрузки. |
| `POST /objects/select` | Потоковый S3 Select для CSV/JSON. |

## Возможности хранилища

`GET /capabilities` принимает необязательные `bucket`, `key`, `versionId`,
`feature`. Без feature возвращается каталог. Область результата включает текущую
сессию, internal/public endpoints, ревизию IAM и запрошенный ресурс.

Статусы: `supported`, `unsupported`, `access_denied`, `unknown`. Поля `evidence`,
`reason`, `checkedAt`, `expiresAt` объясняют происхождение и срок сведений.
`access` отдельно описывает доступ: `allowed`, `denied`, `unknown`.
Подтверждённая конфигурация поддержки протокола не подтверждает права пользователя.

Автоматически выполняются только ограниченные чтения атрибутов и списка
незавершённых загрузок. Записи, Select, checksum-режимы и browser CORS не
проверяются побочными действиями при подключении. При недостатке доказательств
возвращается unknown. Отказ доступа не распространяется на другие сессии.

Кэш: до 1024 результатов и 32 одновременных проверок; одинаковые запросы
объединяются. TTL: supported/unsupported — 5 минут, access_denied — 30 секунд,
unknown — 10 секунд, всегда не дольше сессии. IAM-ревизия отделяет старые сведения,
выход очищает результаты сессии. Кэш не заменяет авторизацию S3.

В этой реализации наблюдения обновляются для multipart create/part/list/complete/abort
и Select. Идентификаторы checksum.*, upload.direct.part и upload.direct.cors остаются
unknown: наличие подписи или локального хэша не используется как доказательство.
Фактические сведения о checksum доступны в receipt и атрибутах объекта.

```sh
curl --fail-with-body --get --cookie "$workdir/cookies" \
  --data-urlencode 'bucket=sdk-api-test' \
  --data-urlencode 'key=sample.csv' \
  --data-urlencode 'feature=object.attributes' \
  "$base/api/sessions/$session_id/capabilities"
```

## Атрибуты и страницы частей

`GET /objects/attributes` требует bucket/key. Необязательные параметры:
`versionId`, `maxParts` (1–1000, по умолчанию 100), `partNumberMarker` (0–10000),
`snapshot` (64 hex-символа).

Ответ содержит фактическую версию, ETag, доступные size/storageClass/checksums,
checksumType, partsCount, parts, truncated и nextPartNumberMarker. Отсутствующее
поле не превращается в подтверждённый ноль. Отсутствующий ObjectParts даёт
parts:null, явно пустой список — parts:[]. ETag не объявляется MD5.

Для конкретной неизменяемой версии возвращается `consistency:version`. Текущий
или `null`-объект получает `consistency:observed`: продолжение требует snapshot
предыдущей страницы, замена наблюдаемого состояния даёт 409 ObjectAttributesChanged.
Это обнаружение изменений по идентификаторам, без атомарного снимка и защиты от ABA.

```sh
curl --fail-with-body --get --cookie "$workdir/cookies" \
  --data-urlencode 'bucket=sdk-api-test' --data-urlencode 'key=large.bin' \
  --data-urlencode 'versionId=VERSION_FROM_S3' --data-urlencode 'maxParts=1' \
  "$base/api/sessions/$session_id/objects/attributes" > "$workdir/attributes.json"

# Только если truncated=true. Сохраняйте bucket/key/versionId/maxParts.
marker=$(jq -er '.nextPartNumberMarker' "$workdir/attributes.json")
snapshot=$(jq -er '.snapshot' "$workdir/attributes.json")
curl --fail-with-body --get --cookie "$workdir/cookies" \
  --data-urlencode 'bucket=sdk-api-test' --data-urlencode 'key=large.bin' \
  --data-urlencode 'versionId=VERSION_FROM_S3' --data-urlencode 'maxParts=1' \
  --data-urlencode "partNumberMarker=$marker" --data-urlencode "snapshot=$snapshot" \
  "$base/api/sessions/$session_id/objects/attributes"
```

## Обычная загрузка с checksum

Прежние обязательные заголовки и поля receipt сохранены. Необязательные заголовки:

- `X-Upload-Checksum-Algorithm`: MD5, SHA1, SHA256, CRC32 или CRC32C.
- `X-Upload-Checksum-Value`: канонический padded base64.
- `X-Upload-Checksum-Encoding`: base64, также значение по умолчанию.
- `X-Upload-Checksum-Type`: FULL_OBJECT, также значение по умолчанию.

При ожидаемой checksum файл сначала проверяется во временном файле общего
бюджета API_TEMPORARY_BYTES. Несовпадение не выполняет запись в S3. Размер файла
ограничен 5 ГиБ и доступным временным бюджетом. Затем применяются прежние условия
назначения и нативный Content-MD5 для PUT либо каждой multipart-части.

```sh
file=sample.csv
size=$(wc -c < "$file" | tr -d ' ')
checksum=$(openssl dgst -sha256 -binary "$file" | openssl base64 -A)
curl --fail-with-body --cookie "$workdir/cookies" \
  -H "Origin: $origin" -H "X-CSRF-Token: $csrf" \
  -H 'X-Object-Revision: absent' -H "X-Upload-Size: $size" \
  -H 'Content-Type: text/csv' \
  -H 'X-Upload-Checksum-Algorithm: SHA256' \
  -H "X-Upload-Checksum-Value: $checksum" \
  --data-binary "@$file" -X PUT \
  "$base/api/sessions/$session_id/objects/upload?bucket=sdk-api-test&key=sample.csv"
```

Receipt.checksum разделяет expected, computed и storage. `api_verified` означает
проверку байтов API; `storage_confirmed` требует совпавшего значения и режима из
ответа хранилища. Неизвестный режим не выводится из ETag. Контрольные суммы ответа
не означают дополнительное перечитывание объекта. Если после подтверждённой записи
ответ содержит противоречащую checksum, возвращаются `ok:false`,
`writeState:committed`, verification:storage_mismatch: не повторяйте запись вслепую.

В прежней конфигурации TrailingHeaders не включён; автоматическая CRC32C SDK не
обещается для каждой загрузки. COMPOSITE через обычный upload отклоняется.
Content-MD5 проверяет транспорт, а не служит криптографической подписью данных.

## Управляемая multipart-загрузка

Новый протокол требует явно проверенной поддержки условий на финальном Complete.
В конфигурации процесса задайте API_CONDITIONAL_COMPLETE_ENDPOINTS списком точных
internal или public endpoints. Пустое значение блокирует создание с
ConditionalCompletionUnverified. Конфигурация не расширяет сетевой allowlist и
не предоставляет прав. Не включайте её только по названию сервера или HTTP 200.

Для одноразового fixture поддержка проверена нативными тестами конфликтов:

```sh
API_LISTEN=127.0.0.1:4173 \
MINIO_ALLOWED_ENDPOINTS=http://127.0.0.1:19000 \
API_CONDITIONAL_COMPLETE_ENDPOINTS=http://127.0.0.1:19000 make run
```

Операция принадлежит сессии; клиент оперирует только непрозрачным ID API.
S3 uploadId, bucket/key и назначение нельзя подменять. Максимум — 5 ГиБ,
части по 16 МиБ, до 320 частей; последняя имеет размер остатка. Пустой файл — одна
часть нулевого размера. Каждая часть требует SHA256 FULL_OBJECT PART в base64.
Необязательная checksum всего плана — SHA256 COMPOSITE OBJECT с суффиксом -N;
это хэш хэшей частей, а не SHA256 конкатенации байтов файла.

Пример для файла до 16 МиБ; бакет sdk-api-test должен существовать в fixture:

```sh
prefix="$base/api/sessions/$session_id"
curl --fail-with-body --cookie "$workdir/cookies" \
  -H "Origin: $origin" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
  --data '{"bucket":"sdk-api-test","key":"managed.csv","revision":"absent","size":7,"contentType":"text/csv"}' \
  "$prefix/objects/multipart" > "$workdir/operation.json"
operation=$(jq -er '.id' "$workdir/operation.json")
printf 'id\n1\n2\n' > "$workdir/part"
checksum=$(openssl dgst -sha256 -binary "$workdir/part" | openssl base64 -A)
curl --fail-with-body --cookie "$workdir/cookies" \
  -H "Origin: $origin" -H "X-CSRF-Token: $csrf" \
  -H 'X-Upload-Size: 7' -H "X-Part-Checksum-SHA256: $checksum" \
  --data-binary "@$workdir/part" -X PUT "$prefix/objects/multipart/$operation/parts/1"

# После обрыва сначала прочитайте состояние: принятые части подтверждаются S3.
curl --fail-with-body --cookie "$workdir/cookies" "$prefix/objects/multipart/$operation"
# Передавайте только отсутствующие части. Другой SHA256 для закреплённого номера запрещён.
curl --fail-with-body --cookie "$workdir/cookies" \
  -H "Origin: $origin" -H "X-CSRF-Token: $csrf" \
  -X POST "$prefix/objects/multipart/$operation/complete"

# Вместо complete: отмена незавершённой операции.
# curl --cookie "$workdir/cookies" -H "Origin: $origin" -H "X-CSRF-Token: $csrf" \
#   -X DELETE "$prefix/objects/multipart/$operation"
```

Состояния включают creating, active, completing, completed, aborting, aborted,
failed, expired и отдельные create_uncertain/complete_uncertain/abort_uncertain.
HTTP 202 обозначает неопределённость, а не подтверждённую запись. Потерянный ответ
create может оставить upload без известного ID; API не пытается присвоить чужую
загрузку из административного списка. Результат потерянного complete проверяется
по маркеру операции, размеру и checksum. Одного наличия ключа недостаточно.
Повтор подтверждённого complete возвращает сохранённый receipt без новой команды.

DELETE сериализован с активной передачей: сначала отмените HTTP-запрос части,
затем отменяйте всю операцию. Logout, TTL и очистка прерывают активное чтение.

Состояние живёт в памяти до конца сессии и не более часа. Возобновление работает
после обрыва соединения, но не после перезапуска API, logout или истечения сессии.
До 256 записей и 16 активных операций на владельца. Очистка отменяет текущую работу
и выполняет до трёх попыток с минутным интервалом; batch ограничен временем и числом
операций. Сетевая ошибка не выдаётся за удаление. После аварии или исчерпания
очистки могут остаться multipart в S3; используйте административный список/Lifecycle.

## Прямая передача части

Для той же операции вместо PUT через API вызовите POST
objects/multipart/{operation}/parts/1/sign с телом:

```json
{"checksum":{"algorithm":"SHA256","encoding":"base64","value":"BASE64_SHA256_OF_PART","type":"FULL_OBJECT","scope":"PART"}}
```

Ответ содержит URL, method, обязательные headers, expiresAt, ID и номер части,
ожидаемый size и sizeEnforcement. Передайте в fetch именно эти URL/method/headers
и Blob части. Нельзя менять host, путь, query или checksum-заголовок после подписи.
Подпись выдаётся credentials текущей сессии для публичного endpoint, до двух минут
и не дольше сессии. Знание ID не даёт доступа другой сессии. Подпись не является
проверкой разрешения S3, окончательная авторизация выполняется хранилищем.

Content-Length не подписывается: браузер устанавливает этот запрещённый для JS
заголовок самостоятельно. Размер проверяется через ListParts перед complete;
oversized-трафик не предотвращается одним этим механизмом. Checksum подписана и
проверяется S3. Повтор URL допускает только закреплённое содержимое части.
Complete и Abort выполняет API, клиентский ответ «загружено» не принимается на веру.

Выход и отмена прекращают выдачу новых URL, но не обещают мгновенный отзыв уже
выданной подписи. Запрос, начатый до expiry, может продолжаться после него.
Поздняя передача не обходит условия финального Complete. CORS автоматически не
меняется: разрешите origin тестового клиента и нужные заголовки на fixture.

Admission API ограничивает передачи через Go и управление операциями. Уже выданные
URL могут использоваться параллельно на S3; этот механизм не задаёт жёсткий лимит
сетевого параллелизма прямых запросов. Для него нужны ограничения самого хранилища.

Подписанные URL содержат временный доступ. Не записывайте их в логи и отчёты.

## S3 Select

POST objects/select?bucket=sdk-api-test&key=sample.csv принимает SQL, input/output
и необязательные maxResultBytes/timeoutSeconds. Поддержаны CSV и JSON LINES/DOCUMENT,
без сжатия. SDK выполняет SelectObjectContent; локальной имитации запросов нет.

```json
{"expression":"SELECT s.name FROM S3Object s WHERE CAST(s.id AS INT) > 1","input":{"csv":{"header":"USE"}},"output":{"json":{"recordDelimiter":"\n"}},"maxResultBytes":8388608,"timeoutSeconds":20}
```

```sh
curl --fail-with-body --no-buffer --cookie "$workdir/cookies" \
  -H "Origin: $origin" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
  --data-binary "@$workdir/select.json" \
  "$prefix/objects/select?bucket=sdk-api-test&key=sample.csv" > "$workdir/select.ndjson"
```

Сохраните предыдущий JSON в select.json. В потоке data:{encoding:base64,bytes:...}
декодируйте каждое значение bytes отдельно, затем соединяйте полученные байты.
Чанк не является целой CSV/JSON-записью или целым символом UTF-8. Base64-строки
нельзя просто склеивать из-за padding. Единственный подтверждённый конец —
result.complete:true. Терминальный error означает неполные данные даже при HTTP 200.
statistics публикуется только после завершения, если хранилище её предоставило.

Лимиты: SQL 16 КиБ; тело 32 КиБ; проверенный HEAD размер исходника до 256 МиБ;
результат до 32 МиБ (по умолчанию 8); время до 30 секунд (по умолчанию 20);
событие API до 32 КиБ декодированных байтов. Upstream: кадр 1 МиБ, заголовки 8 КиБ,
служебное событие 64 КиБ, поток 64 МиБ/8192 события. Кадры и CRC проверяются до SDK.
Остальные POST и метаданные сохраняют прежние пределы 4 МиБ/64 КиБ ошибок.

VersionID и Range не поддерживаются. Проверки до/после Select обнаруживают
наблюдаемую замену, но не закрепляют объект атомарно и не защищают от ABA. Лимит
исходника относится к проверенному HEAD; при внешней гонке это не жёсткая граница
сканирования. Отмена обработки уже принятых сервером данных — best effort.

Дедлайн Select прерывает также блокирующую запись/Flush клиенту. Для терминальной
ошибки после истечения времени выполнения отводится до одной дополнительной секунды;
отказ доставки не приводит к повторному HTTP-ответу или терминальному success.

## Совместимость и проверки

High-level multipart SDK v7.4.1 теряет custom If-* на Complete. Storage-адаптер
восстанавливает их только в финальном запросе соответствующей загрузки. Эти
восстановленные заголовки действуют как нативные preconditions, но не входят в
SignedHeaders; Core Complete подписывает переданные условия обычным способом.
If-Match сравнивает ETag; это не атомарный CAS всех метаданных или VersionID.
Ответ PUT с неожиданным телом или без ETag даёт S3WriteUnconfirmed: запись могла
состояться, сначала проверьте состояние. Автоматического повтора нет.

Реальные интеграционные проверки выполняются только с одноразовым
RELEASE.2025-09-07T16-13-09Z из docker-compose.test.yml. Это не подтверждение рабочего
сервера пользователя или других S3-совместимых реализаций. Протокольные тесты с
httptest проверяют ошибки/гонки отдельно от нативных сценариев.

```sh
make docs
make check
make fixtures-up
make test-integration
make fixtures-down
```
