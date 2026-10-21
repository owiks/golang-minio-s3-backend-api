package integration

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestEmptyFilePreviewIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	const key = "empty + %.txt"
	if _, err := client.PutObject(context.Background(), bucket, key, strings.NewReader(""), 0, minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	data := c.call("GET", "objects/preview?"+url.Values{"bucket": {bucket}, "key": {key}}.Encode(), nil, 200)
	if len(data) != 0 {
		t.Fatalf("empty preview returned %q", data)
	}
}

func TestGroupReadContractsIntegration(t *testing.T) {
	c, _, _ := fileFixture(t)
	user, group := "audit-user-"+strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10])), "audit-group-"+strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	c.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Audit-group-fixture-secret-123"}, 200)
	defer c.call("DELETE", "admin/users/"+user, map[string]string{"confirmation": user}, 200)
	c.call("POST", "admin/groups", map[string]any{"acknowledgeNonAtomicCreate": true, "name": group, "members": []string{user}}, 200)
	defer func() {
		c.call("POST", "admin/groups/"+group+"/members", map[string]any{"members": []string{user}, "remove": true}, 200)
		c.call("DELETE", "admin/groups/"+group, map[string]string{"confirmation": group}, 200)
	}()
	c.call("POST", "admin/bindings", map[string]any{"group": group, "policy": "readonly"}, 200)
	defer c.call("POST", "admin/bindings", map[string]any{"group": group, "policy": "readonly", "remove": true}, 200)
	var list []string
	if err := json.Unmarshal(c.call("GET", "admin/groups", nil, 200), &list); err != nil || !slices.Contains(list, group) {
		t.Fatal("group missing from list", list, err)
	}
	var detail struct {
		Name              string
		Members, Policies []string
	}
	if err := json.Unmarshal(c.call("GET", "admin/groups/"+group, nil, 200), &detail); err != nil || detail.Name != group || !slices.Equal(detail.Members, []string{user}) || !slices.Equal(detail.Policies, []string{"readonly"}) {
		t.Fatal("incorrect group details", detail, err)
	}
	reader := loginTest(t, c.Origin, user, "Audit-group-fixture-secret-123")
	reader.call("GET", "admin/groups", nil, 403)
	reader.call("GET", "admin/groups/"+group, nil, 403)
	c.call("POST", "admin/groups/"+group+"/members", map[string]any{"members": []string{user}, "remove": true}, 200)
	var empty map[string]json.RawMessage
	_ = json.Unmarshal(c.call("GET", "admin/groups/"+group, nil, 200), &empty)
	if string(empty["members"]) != "[]" {
		t.Fatal("empty group members must be an array", string(empty["members"]))
	}
}
