package api

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	accessroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/access"
	archivesroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/archives"
	attributesroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/attributes"
	authenticationroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/authentication"
	batchroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/batch"
	bindingsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/bindings"
	bucketsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/buckets"
	capabilitiesroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/capabilities"
	foldersroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/folders"
	groupsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/groups"
	healthroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/health"
	keysroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/keys"
	multipartroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/multipart"
	objectsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/objects"
	operationsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operations"
	policiesroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/policies"
	searchroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/search"
	selectqueryroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/selectquery"
	serverroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/server"
	sessionsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/sessions"
	tiersroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/tiers"
	transfersroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/transfers"
	uploadsroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/uploads"
	usersroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/users"
)

var registry = func() *routes.Registry {
	var result []routes.APIRoute
	result = append(result, healthroutes.GetRoutes()...)
	result = append(result, sessionsroutes.GetRoutes()...)
	result = append(result, authenticationroutes.GetRoutes()...)
	result = append(result, accessroutes.GetRoutes()...)
	result = append(result, capabilitiesroutes.GetRoutes()...)
	result = append(result, objectsroutes.GetRoutes()...)
	result = append(result, operationsroutes.GetRoutes()...)
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
	for i, route := range result {
		for j, op := range route.Operations {
			scopes, ok := routes.ScopeContract(route, op)
			if !ok {
				panic("missing route scope contract: " + route.Path + "/" + op.Suffix)
			}
			result[i].Operations[j].Scopes = scopes
		}
	}
	registry, err := routes.NewRegistry(result)
	if err != nil {
		panic(err)
	}
	return registry
}()

func Catalog() []routes.APIRoute { return registry.Catalog() }
