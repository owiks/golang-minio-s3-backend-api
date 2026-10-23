package authentication

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/authentication/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{Path: "credential", Tag: "sessions", Operations: []routes.Operation{
			{Method: "GET", Controller: "CurrentCredential", Summary: "Текущий доступ и ограничения", Input: routes.Inputs().Describe("effectiveOperations содержит действующие ограничения операций API без операторского секрета. maxTokenTTLSeconds — настроенный API_TOKEN_TTL в секундах, верхний предел положительного ttlSeconds Bearer-токена. ttlSeconds=0 — без собственного ограничения срока; конечный срок родительского Bearer всё равно действует. expiresAt=0001-01-01T00:00:00Z означает отсутствие срока. Права доступа и IAM проверяются отдельно."), Response: controllers.CurrentView{}, Handler: controllers.Current},
		}},
		{Path: "connections", Tag: "sessions", Operations: []routes.Operation{
			{Method: "GET", Controller: "ListConnections", Summary: "Сохранённые подключения владельца", Input: routes.Inputs(), Response: []authstate.ConnectionView{}, Handler: controllers.Connections},
			{Suffix: "{connectionID}", Method: "GET", Controller: "GetConnection", Summary: "Сохранённое подключение", Input: routes.Inputs(), Response: authstate.ConnectionView{}, Handler: controllers.Connections},
			{Suffix: "{connectionID}", Method: "DELETE", Controller: "DeleteConnection", Summary: "Отзыв подключения и зависимых доступов", Input: routes.Inputs().Describe("Отзывает все доступы подключения и их потомков, в том числе в других подключениях того же владельца. Удаляет зашифрованные ключи."), Request: controllers.RevisionRequest{}, Response: common.Success{}, Handler: controllers.Connections},
			{Suffix: "{connectionID}/credentials", Method: "PUT", Controller: "ReplaceConnectionCredentials", Summary: "Замена ключей S3 с сохранением API-доступов", Input: routes.Inputs().Describe("Новые ключи должны принадлежать тому же upstream-пользователю и пройти проверку хранилища. ID, секреты API-доступов, сроки и права сохраняются. Следующие запросы используют новые ключи."), Request: controllers.CredentialsRequest{}, Response: common.Success{}, Handler: controllers.Credentials},
			{Suffix: "{connectionID}/tokens", Method: "GET", Controller: "ListConnectionTokens", Summary: "Токены подключения без секретов", Input: routes.Inputs(), Response: []authstate.AccessView{}, Handler: controllers.Tokens},
			{Suffix: "{connectionID}/tokens", Method: "POST", Controller: "IssueConnectionToken", Summary: "Выдача ограниченного Bearer-токена", Input: routes.Inputs().Describe("policy обязателен; права не шире вызывающего доступа. Через Bearer создаётся дочерний токен с parentID и сроком не дольше родителя; через cookie — самостоятельный сервисный токен. ttlSeconds обязателен: 0 — без собственного ограничения срока, положительный срок ограничен API_TOKEN_TTL. Cookie не ограничивает срок самостоятельного токена. Бессрочный ответ: expiresAt=0001-01-01T00:00:00Z и expiresIn=0. Секрет возвращается один раз."), Request: controllers.TokenIssueRequest{}, Response: authstate.Issued{}, Status: 201, Handler: controllers.Tokens},
		}},
		{Path: "tokens", Tag: "sessions", Operations: []routes.Operation{
			{Suffix: "{tokenID}", Method: "GET", Controller: "GetConnectionToken", Summary: "Сведения о токене без секрета", Input: routes.Inputs(), Response: authstate.AccessView{}, Handler: controllers.GetToken},
			{Suffix: "{tokenID}", Method: "PATCH", Controller: "EditConnectionToken", Summary: "Название и сужение прав токена", Input: routes.Inputs().Describe("Сужение прав атомарно распространяется на потомков. При пустом пересечении прав или ресурсов дочерний токен отзывается."), Request: authstate.EditRequest{}, Response: authstate.AccessView{}, Handler: controllers.Edit},
			{Suffix: "{tokenID}", Method: "DELETE", Controller: "RevokeConnectionToken", Summary: "Отзыв токена и его потомков", Input: routes.Inputs(), Request: controllers.RevisionRequest{}, Response: common.Success{}, Handler: controllers.Revoke},
			{Suffix: "{tokenID}/rotate", Method: "POST", Controller: "RotateConnectionToken", Summary: "Ротация секрета без смены ID и срока", Input: routes.Inputs().Describe("revision обязателен. graceSeconds от 0 до 60, по умолчанию 0. Старый секрет в период перекрытия не управляет доступами."), Request: authstate.RotateRequest{}, Response: authstate.Issued{}, Handler: controllers.Rotate},
			{Suffix: "{tokenID}/renew", Method: "POST", Controller: "RenewConnectionToken", Summary: "Продление действующего Bearer-токена", Input: routes.Inputs().Describe("Собственный токен с renewable=true продлевается без access:manage, с текущими секретом и revision, ttlSeconds обязателен: 0 снимает собственное ограничение срока, положительный срок — до текущего времени + API_TOKEN_TTL; дочерний — не дольше родителя. Для другого токена нужны access:manage и достаточные права; срок ограничен вызывающим Bearer, а cookie не ограничивает срок сервисного токена. ID, секрет и права сохраняются. Сокращение срока распространяется на потомков. Истёкшие и отозванные токены не восстанавливаются; cookie не продлевается. Старые записи с renewable=false не получают самостоятельного продления."), Request: controllers.TokenRenewRequest{}, Response: authstate.AccessView{}, Handler: controllers.Renew},
		}},
		{Path: "auth-events", Tag: "sessions", Operations: []routes.Operation{
			{Method: "GET", Controller: "AccessAuditEvents", Summary: "Аудит управления доступами владельца", Input: routes.Inputs(), Response: []authstate.Event{}, Handler: controllers.Events},
		}},
	}
}
