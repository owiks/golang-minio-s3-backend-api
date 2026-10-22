package doc

import (
	"context"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/openapi"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
)

// Build constructs and validates documentation without adding runtime request or
// response validation. Service validation and streaming handlers remain owners
// of their existing HTTP contracts.
func Build(catalog []routes.APIRoute) *openapi3.T {
	d := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "MinIO S3 Backend API", Version: "1.0.0", Description: apiDescription},
		Servers: openapi3.Servers{{URL: "/"}},
		Paths:   openapi3.NewPaths(),
		Components: &openapi3.Components{
			SecuritySchemes: openapi3.SecuritySchemes{
				"SessionCookie":  {Value: &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "Cookie", Description: "Cookie minio_session_{sessionID}, полученная после подключения. Браузер передаёт её автоматически; JavaScript не читает HttpOnly cookie."}},
				"CSRF":           {Value: &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "X-CSRF-Token", Description: "csrf из ответа POST /api/sessions; изменения также требуют Origin интерфейса."}},
				"SessionBearer":  {Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer", Description: "token из POST /api/tokens или connections/{connectionID}/tokens, для соответствующего id в пути сессии. Не требует cookie, Origin и CSRF. Неверный Authorization возвращает 401 без перехода к cookie. Не является токеном приглашения/общей папки или STS-токеном MinIO."}},
				"BearerGrant":    {Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer", Description: "Токен приглашения загрузки или общей папки. Не сохраняется в Swagger UI."}},
				"OperatorBearer": {Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer", Description: "Отдельный API_OPERATOR_TOKEN оператора API. Не является пользовательским токеном или ключом MinIO. Только чтение сводки фоновой обработки."}},
			},
			Schemas: openapi3.Schemas{"Error": {Value: errorSchema()}},
		},
	}
	for _, route := range catalog {
		for _, op := range route.Operations {
			path := route.Template(op)
			documented := newOperation(path, route.Tag, op.Method, op.Summary, route.Public, d.Components.Schemas["Error"])
			applyInput(documented, op)
			if op.Operator {
				security := openapi3.SecurityRequirements{{"OperatorBearer": {}}}
				documented.Security = &security
				documented.Responses.Value("401").Value.Headers["WWW-Authenticate"] = header(openapi3.NewStringSchema(), "Bearer realm=\"minio-s3-operator\", error=\"invalid_token\" при неверном операторском токене.")
			}
			if len(op.Scopes) > 0 {
				documented.Extensions = map[string]any{"x-api-scopes": append([]string(nil), op.Scopes...)}
			}
			if op.Request != nil {
				schema := openapi.TypeSchema(op.Request, true)
				if len(op.Required) > 0 {
					schema.Value.Required = append([]string(nil), op.Required...)
					for _, name := range op.Required {
						property := schema.Value.Properties[name]
						if property == nil {
							panic(fmt.Sprintf("unknown required request field: %s %s %s", op.Method, path, name))
						}
						property.Value.Nullable = false
					}
				}
				documented.RequestBody = &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Required: true, Content: content("application/json", schema)}}
			}
			if op.BinaryRequest {
				documented.RequestBody = &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Required: true, Content: content("application/octet-stream", binarySchema())}}
			}
			applyResponses(documented, op)
			for _, response := range documented.Responses.Map() {
				if response.Value.Headers == nil {
					response.Value.Headers = openapi3.Headers{}
				}
				if response.Value.Headers["X-Request-ID"] == nil {
					response.Value.Headers["X-Request-ID"] = header(openapi3.NewStringSchema(), "")
				}
				if response.Value.Content["application/json"] != nil || response.Value.Content["application/x-ndjson"] != nil {
					localizedResponseHeaders(response.Value)
				}
			}
			item := d.Paths.Value(path)
			if item == nil {
				item = &openapi3.PathItem{}
				d.Paths.Set(path, item)
			}
			if item.GetOperation(op.Method) != nil {
				panic(fmt.Sprintf("duplicate API operation: %s %s", op.Method, path))
			}
			item.SetOperation(op.Method, documented)
		}
	}
	if err := d.Validate(context.Background()); err != nil {
		panic(fmt.Sprintf("invalid API documentation: %v", err))
	}
	return d
}

func errorSchema() *openapi3.Schema {
	problem := openapi3.NewObjectSchema().WithProperty("code", openapi3.NewStringSchema()).WithProperty("message", openapi3.NewStringSchema()).WithRequired([]string{"code", "message"})
	return openapi3.NewObjectSchema().WithProperty("error", problem).WithRequired([]string{"error"})
}

func content(media string, schema *openapi3.SchemaRef) openapi3.Content {
	return openapi3.Content{media: {Schema: schema}}
}

func binarySchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Value: openapi3.NewStringSchema().WithFormat("binary")}
}

func header(schema *openapi3.Schema, description string) *openapi3.HeaderRef {
	return &openapi3.HeaderRef{Value: &openapi3.Header{Parameter: openapi3.Parameter{Description: description, Schema: &openapi3.SchemaRef{Value: schema}}}}
}

func localizedResponseHeaders(response *openapi3.Response) {
	if response.Headers == nil {
		response.Headers = openapi3.Headers{}
	}
	response.Headers["Content-Language"] = header(openapi3.NewStringSchema().WithEnum("en", "ru"), "Язык сообщений API; не меняет содержимое файлов.")
	response.Headers["Vary"] = header(openapi3.NewStringSchema(), "В режиме auto содержит Accept-Language.")
}

func newOperation(path, tag, method, summary string, public bool, problem *openapi3.SchemaRef) *openapi3.Operation {
	security := openapi3.SecurityRequirements{}
	if !public {
		requirement := openapi3.SecurityRequirement{"SessionCookie": {}}
		if method != "GET" {
			requirement["CSRF"] = []string{}
		}
		security = append(security, requirement, openapi3.SecurityRequirement{"SessionBearer": {}})
	}
	op := &openapi3.Operation{OperationID: strings.ToLower(method) + "_" + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(strings.Trim(path, "/")), Tags: []string{tag}, Summary: summary, Security: &security, Responses: openapi3.NewResponses()}
	op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Value: openapi3.NewHeaderParameter("Accept-Language").WithDescription("Язык сообщений в режиме API_LANGUAGE=auto: en или ru, включая региональные варианты и приоритеты q. Без подходящего значения — en.").WithSchema(openapi3.NewStringSchema())})
	if !public && method != "GET" {
		op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Value: openapi3.NewHeaderParameter("Origin").WithDescription("Обязателен при cookie-авторизации: адрес интерфейса должен пройти SameOrigin. Для SessionBearer не требуется.").WithSchema(openapi3.NewStringSchema())})
	}
	for _, code := range []string{"400", "401", "403", "404", "409", "410", "413", "415", "422", "429", "500", "501", "502", "503", "504"} {
		response := openapi3.NewResponse().WithDescription("Ошибка; code и message содержат безопасное описание.").WithContent(content("application/json", &openapi3.SchemaRef{Ref: "#/components/schemas/Error", Value: problem.Value}))
		response.Headers = openapi3.Headers{"X-Request-ID": header(openapi3.NewStringSchema(), "Идентификатор запроса для поиска в логах API.")}
		op.Responses.Set(code, &openapi3.ResponseRef{Value: response})
	}
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") {
			op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Value: openapi3.NewPathParameter(strings.Trim(part, "{}")).WithSchema(openapi3.NewStringSchema())})
		}
	}
	op.Responses.Value("429").Value.Headers["Retry-After"] = header(openapi3.NewIntegerSchema(), "Секунды до повтора; передаётся при ограничении попыток входа.")
	if !public {
		op.Responses.Value("401").Value.Headers["WWW-Authenticate"] = header(openapi3.NewStringSchema(), "Bearer realm=\"minio-s3-api\", error=\"invalid_token\" при недействительном, истёкшем или отозванном токене.")
	}
	return op
}

func applyResponses(documented *openapi3.Operation, op routes.Operation) {
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
		current := documented.Responses.Value(code)
		if current == nil {
			current = &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("Успешный ответ.")}
			documented.Responses.Set(code, current)
		}
		if response.MediaType == "" {
			continue
		}
		var schema *openapi3.SchemaRef
		switch {
		case response.Binary:
			schema = binarySchema()
		case len(response.Variants) > 0:
			variants := openapi3.SchemaRefs{}
			for _, model := range response.Variants {
				variants = append(variants, openapi.TypeSchema(model, false))
			}
			// Stream records can overlap; anyOf does not claim exclusivity.
			schema = &openapi3.SchemaRef{Value: &openapi3.Schema{AnyOf: variants}}
		default:
			schema = openapi.TypeSchema(response.Model, false)
		}
		if current.Value.Content == nil {
			current.Value.Content = openapi3.Content{}
		}
		current.Value.Content[response.MediaType] = &openapi3.MediaType{Schema: schema}
		if response.Localized {
			localizedResponseHeaders(current.Value)
		}
		if response.MediaType == "application/x-ndjson" {
			documented.Description += " Поток NDJSON: каждая строка содержит прогресс, результат или ошибку операции."
		}
	}
}

// Input is declared per method, so shared controllers cannot mix action inputs.
func applyInput(documented *openapi3.Operation, op routes.Operation) {
	documented.Description = op.Input.Description
	for _, p := range op.Input.Parameters {
		if p.In == "header" && strings.EqualFold(p.Name, "Authorization") {
			security := openapi3.SecurityRequirements{{"BearerGrant": {}}}
			documented.Security = &security
			continue
		}
		schema := &openapi3.Schema{Type: &openapi3.Types{p.Type}, Format: p.Format}
		for _, value := range p.Enum {
			schema.Enum = append(schema.Enum, value)
		}
		if p.Minimum != nil {
			value := float64(*p.Minimum)
			schema.Min = &value
		}
		if p.Maximum != nil {
			value := float64(*p.Maximum)
			schema.Max = &value
		}
		description := p.Description
		if p.Required && p.Type == "string" {
			schema.MinLength = 1
		}
		if p.MaxBytes > 0 {
			schema.Extensions = map[string]any{"x-maxBytes": p.MaxBytes}
			if description != "" {
				description += " "
			}
			description += fmt.Sprintf("Не более %d байт UTF-8.", p.MaxBytes)
		}
		documented.Parameters = append(documented.Parameters, &openapi3.ParameterRef{Value: &openapi3.Parameter{Name: p.Name, In: p.In, Required: p.Required, Description: description, Schema: &openapi3.SchemaRef{Value: schema}}})
	}
}

const apiDescription = "HTTP API для S3-совместимых хранилищ и MinIO Admin API. Для веба: POST /api/sessions выдаёт HttpOnly cookie minio_session_{sessionID}; изменения требуют Origin и X-CSRF-Token. Для скриптов: POST /api/tokens выдаёт отдельные id, token и expiresAt; передавайте Authorization: Bearer <token> на тех же /api/sessions/{id}/... без cookie и CSRF. DELETE /api/sessions/{id} отзывает токен. При API_DB_DRIVER=postgres/mysql/sqlite подключения и доступы сохраняются между перезапусками; memory сохраняет прежний временный режим. Управление: connections, connections/{connectionID}/tokens, tokens/{tokenID}/rotate и renew. API дополнительно ограничивает scopes и bucket/prefix; MinIO сохраняет окончательную проверку прав. Передавайте ключи и токены через HTTPS. Swagger UI служит документацией; выполнение запросов отключено. Право на каждую операцию окончательно проверяет MinIO. Ключи и токены не сохраняются в документации."
