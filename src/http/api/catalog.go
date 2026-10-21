package api

import (
	"minio-web/api/src/http/api/routes"
	accessroutes "minio-web/api/src/http/api/routes/access"
	archivesroutes "minio-web/api/src/http/api/routes/archives"
	attributesroutes "minio-web/api/src/http/api/routes/attributes"
	batchroutes "minio-web/api/src/http/api/routes/batch"
	bindingsroutes "minio-web/api/src/http/api/routes/bindings"
	bucketsroutes "minio-web/api/src/http/api/routes/buckets"
	capabilitiesroutes "minio-web/api/src/http/api/routes/capabilities"
	foldersroutes "minio-web/api/src/http/api/routes/folders"
	groupsroutes "minio-web/api/src/http/api/routes/groups"
	healthroutes "minio-web/api/src/http/api/routes/health"
	keysroutes "minio-web/api/src/http/api/routes/keys"
	multipartroutes "minio-web/api/src/http/api/routes/multipart"
	objectsroutes "minio-web/api/src/http/api/routes/objects"
	policiesroutes "minio-web/api/src/http/api/routes/policies"
	searchroutes "minio-web/api/src/http/api/routes/search"
	selectqueryroutes "minio-web/api/src/http/api/routes/selectquery"
	serverroutes "minio-web/api/src/http/api/routes/server"
	sessionsroutes "minio-web/api/src/http/api/routes/sessions"
	tiersroutes "minio-web/api/src/http/api/routes/tiers"
	transfersroutes "minio-web/api/src/http/api/routes/transfers"
	uploadsroutes "minio-web/api/src/http/api/routes/uploads"
	usersroutes "minio-web/api/src/http/api/routes/users"
)

var registry = func() *routes.Registry {
	var result []routes.APIRoute
	result = append(result, healthroutes.GetRoutes()...)
	result = append(result, sessionsroutes.GetRoutes()...)
	result = append(result, accessroutes.GetRoutes()...)
	result = append(result, capabilitiesroutes.GetRoutes()...)
	result = append(result, objectsroutes.GetRoutes()...)
	result = append(result, attributesroutes.GetRoutes()...)
	result = append(result, foldersroutes.GetRoutes()...)
	result = append(result, transfersroutes.GetRoutes()...)
	result = append(result, searchroutes.GetRoutes()...)
	result = append(result, selectqueryroutes.GetRoutes()...)
	result = append(result, uploadsroutes.GetRoutes()...)
	result = append(result, multipartroutes.GetRoutes()...)
	result = append(result, archivesroutes.GetRoutes()...)
	result = append(result, serverroutes.GetRoutes()...)
	result = append(result, tiersroutes.GetRoutes()...)
	result = append(result, groupsroutes.GetRoutes()...)
	result = append(result, policiesroutes.GetRoutes()...)
	result = append(result, bucketsroutes.GetRoutes()...)
	result = append(result, usersroutes.GetRoutes()...)
	result = append(result, bindingsroutes.GetRoutes()...)
	result = append(result, keysroutes.GetRoutes()...)
	result = append(result, batchroutes.GetRoutes()...)
	registry, err := routes.NewRegistry(result)
	if err != nil {
		panic(err)
	}
	return registry
}()

func Catalog() []routes.APIRoute { return registry.Catalog() }
