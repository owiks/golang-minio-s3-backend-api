package doc

import (
	"fmt"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/openapi"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
)

func Build(catalog []routes.APIRoute) openapi.Document {
	d := openapi.Document{OpenAPI: "3.0.3", Info: map[string]string{
		"title": "MinIO S3 Backend API", "version": "1.0.0",
		"description": "HTTP API для S3-совместимых хранилищ и MinIO Admin API. Для веба: POST /api/sessions выдаёт HttpOnly cookie minio_session_{sessionID}; изменения требуют Origin и X-CSRF-Token. Для скриптов: POST /api/tokens выдаёт отдельные id, token и expiresAt; передавайте Authorization: Bearer <token> на тех же /api/sessions/{id}/... без cookie и CSRF. DELETE /api/sessions/{id} отзывает токен. Токены живут в памяти до истечения срока, отзыва или перезапуска API. Передавайте ключи и токены через HTTPS. Swagger UI служит документацией; выполнение запросов отключено. Право на каждую операцию окончательно проверяет MinIO. Ключи и токены не сохраняются в документации.",
	}, Servers: []map[string]string{{"url": "/"}}, Paths: map[string]map[string]openapi.Operation{}, Components: map[string]any{
		"securitySchemes": map[string]any{
			"SessionCookie": map[string]any{"type": "apiKey", "in": "header", "name": "Cookie", "description": "Cookie minio_session_{sessionID}, полученная после подключения. Браузер передаёт её автоматически; JavaScript не читает HttpOnly cookie."},
			"CSRF":          map[string]any{"type": "apiKey", "in": "header", "name": "X-CSRF-Token", "description": "csrf из ответа POST /api/sessions; изменения также требуют Origin интерфейса."},
			"SessionBearer": map[string]any{"type": "http", "scheme": "bearer", "description": "token из POST /api/tokens, для соответствующего id в пути сессии. Не требует cookie, Origin и CSRF. Неверный Authorization возвращает 401 без перехода к cookie. Не является токеном приглашения/общей папки или STS-токеном MinIO."},
			"BearerGrant":   map[string]any{"type": "http", "scheme": "bearer", "description": "Токен приглашения загрузки или общей папки. Не сохраняется в Swagger UI."},
		}, "schemas": map[string]any{"Error": map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{"error": map[string]any{"type": "object", "required": []string{"code", "message"}, "properties": map[string]any{"code": map[string]string{"type": "string"}, "message": map[string]string{"type": "string"}}}}}},
	}}
	for _, route := range catalog {
		for _, op := range route.Operations {
			path := route.Template(op)
			add(&d, path, route.Tag, op.Method, op.Summary, route.Public)
			documented := d.Paths[path][strings.ToLower(op.Method)]
			applyInput(&documented, op)
			if op.Request != nil {
				schema := openapi.TypeSchema(op.Request, true)
				if len(op.Required) > 0 {
					schema["required"] = op.Required
					for _, name := range op.Required {
						property, exists := schema["properties"].(map[string]any)[name]
						if !exists {
							panic(fmt.Sprintf("unknown required request field: %s %s %s", op.Method, path, name))
						}
						if property, ok := property.(openapi.Schema); ok {
							delete(property, "nullable")
						}
					}
				}
				documented.RequestBody = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
			}
			if op.BinaryRequest {
				documented.RequestBody = map[string]any{"required": true, "content": map[string]any{"application/octet-stream": map[string]any{"schema": openapi.Schema{"type": "string", "format": "binary"}}}}
			}
			applyResponses(&documented, op)
			for _, response := range documented.Responses {
				if value, ok := response.(map[string]any); ok {
					headers := responseHeaders(value)
					if headers["X-Request-ID"] == nil {
						headers["X-Request-ID"] = map[string]any{"schema": map[string]string{"type": "string"}}
					}
					content, _ := value["content"].(map[string]any)
					_, json := content["application/json"]
					_, ndjson := content["application/x-ndjson"]
					if json || ndjson {
						localizedResponseHeaders(value)
					}
				}
			}
			d.Paths[path][strings.ToLower(op.Method)] = documented
		}
	}
	return d
}

func responseHeaders(response map[string]any) map[string]any {
	headers, ok := response["headers"].(map[string]any)
	if !ok {
		headers = map[string]any{}
		response["headers"] = headers
	}
	return headers
}

func localizedResponseHeaders(response map[string]any) {
	headers := responseHeaders(response)
	headers["Content-Language"] = map[string]any{"schema": map[string]any{"type": "string", "enum": []string{"en", "ru"}}, "description": "Язык сообщений API; не меняет содержимое файлов."}
	headers["Vary"] = map[string]any{"schema": map[string]string{"type": "string"}, "description": "В режиме auto содержит Accept-Language."}
}

func add(d *openapi.Document, path, tag, method, summary string, public bool) {
	security := []map[string][]string{}
	if !public {
		requirements := map[string][]string{"SessionCookie": {}}
		if method != "GET" {
			requirements["CSRF"] = []string{}
		}
		security = append(security, requirements)
		security = append(security, map[string][]string{"SessionBearer": {}})
	}
	op := openapi.Operation{OperationID: strings.ToLower(method) + "_" + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(strings.Trim(path, "/")), Tags: []string{tag}, Summary: summary, Security: security, Responses: map[string]any{}}
	op.Parameters = append(op.Parameters, openapi.Parameter{Name: "Accept-Language", In: "header", Description: "Язык сообщений в режиме API_LANGUAGE=auto: en или ru, включая региональные варианты и приоритеты q. Без подходящего значения — en.", Schema: openapi.Schema{"type": "string"}})
	if !public && method != "GET" {
		op.Parameters = append(op.Parameters, openapi.Parameter{Name: "Origin", In: "header", Description: "Обязателен при cookie-авторизации: адрес интерфейса должен пройти SameOrigin. Для SessionBearer не требуется.", Schema: openapi.Schema{"type": "string"}})
	}
	for _, code := range []string{"400", "401", "403", "404", "409", "410", "413", "415", "422", "429", "500", "501", "502", "503", "504"} {
		op.Responses[code] = map[string]any{"description": "Ошибка; code и message содержат безопасное описание.", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/Error"}}}}
	}
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") {
			op.Parameters = append(op.Parameters, openapi.Parameter{Name: strings.Trim(part, "{}"), In: "path", Required: true, Schema: openapi.Schema{"type": "string"}})
		}
	}
	if d.Paths[path] == nil {
		d.Paths[path] = map[string]openapi.Operation{}
	}
	key := strings.ToLower(method)
	if _, exists := d.Paths[path][key]; exists {
		panic(fmt.Sprintf("duplicate API operation: %s %s", method, path))
	}
	for _, response := range op.Responses {
		if value, ok := response.(map[string]any); ok {
			value["headers"] = map[string]any{"X-Request-ID": map[string]any{"description": "Идентификатор запроса для поиска в логах API.", "schema": map[string]string{"type": "string"}}}
		}
	}
	if value, ok := op.Responses["429"].(map[string]any); ok {
		value["headers"].(map[string]any)["Retry-After"] = map[string]any{"description": "Секунды до повтора; передаётся при ограничении попыток входа.", "schema": map[string]string{"type": "integer"}}
	}
	if !public {
		op.Responses["401"].(map[string]any)["headers"].(map[string]any)["WWW-Authenticate"] = map[string]any{"schema": map[string]string{"type": "string"}, "description": "Bearer realm=\"minio-s3-api\", error=\"invalid_token\" при недействительном, истёкшем или отозванном токене."}
	}
	d.Paths[path][key] = op
}

func applyResponses(documented *openapi.Operation, op routes.Operation) {
	for key := range documented.Responses {
		if strings.HasPrefix(key, "2") {
			delete(documented.Responses, key)
		}
	}
	responses := op.Responses
	if op.Response != nil {
		status := op.Status
		if status == 0 {
			status = 200
		}
		responses = []routes.Response{{Status: status, MediaType: "application/json", Model: op.Response}}
	}
	for _, response := range responses {
		code := fmt.Sprint(response.Status)
		current, ok := documented.Responses[code].(map[string]any)
		if !ok {
			current = map[string]any{"description": "Успешный ответ."}
			documented.Responses[code] = current
		}
		if response.MediaType == "" {
			continue
		}
		var schema openapi.Schema
		switch {
		case response.Binary:
			schema = openapi.Schema{"type": "string", "format": "binary"}
		case len(response.Variants) > 0:
			variants := []openapi.Schema{}
			for _, model := range response.Variants {
				variants = append(variants, openapi.TypeSchema(model, false))
			}
			// Variants can overlap (for example partial errors); anyOf describes that
			// truthfully without asserting mutually exclusive schemas.
			schema = openapi.Schema{"anyOf": variants}
		default:
			schema = openapi.TypeSchema(response.Model, false)
		}
		content, ok := current["content"].(map[string]any)
		if !ok {
			content = map[string]any{}
			current["content"] = content
		}
		content[response.MediaType] = map[string]any{"schema": schema}
		if response.Localized {
			localizedResponseHeaders(current)
		}
		if response.MediaType == "application/x-ndjson" {
			documented.Description += " Поток NDJSON: каждая строка содержит прогресс, результат или ошибку операции."
		}
	}
}

// Input is declared per method, so a shared controller cannot leak another
// action's parameters or body into this operation's documentation.
func applyInput(documented *openapi.Operation, op routes.Operation) {
	documented.Description = op.Input.Description
	for _, p := range op.Input.Parameters {
		if p.In == "header" && strings.EqualFold(p.Name, "Authorization") {
			documented.Security = []map[string][]string{{"BearerGrant": {}}}
			continue
		}
		schema := openapi.Schema{"type": p.Type}
		if p.Format != "" {
			schema["format"] = p.Format
		}
		if len(p.Enum) > 0 {
			schema["enum"] = p.Enum
		}
		if p.Minimum != nil {
			schema["minimum"] = *p.Minimum
		}
		if p.Maximum != nil {
			schema["maximum"] = *p.Maximum
		}
		description := p.Description
		if p.Required && p.Type == "string" {
			schema["minLength"] = 1
		}
		if p.MaxBytes > 0 {
			schema["x-maxBytes"] = p.MaxBytes
			if description != "" {
				description += " "
			}
			description += fmt.Sprintf("Не более %d байт UTF-8.", p.MaxBytes)
		}
		documented.Parameters = append(documented.Parameters, openapi.Parameter{Name: p.Name, In: p.In, Required: p.Required, Description: description, Schema: schema})
	}
}
