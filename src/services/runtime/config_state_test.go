package runtime

import (
	"net/netip"
	"reflect"
	"sync"
	"testing"

	"minio-web/api/src/config"
)

func TestRuntimeOwnsIndependentConfiguration(t *testing.T) {
	cfg := config.Default()
	cfg.Origin = "https://ui.example"
	cfg.Endpoints.Allowed["https://s3.example"] = true
	cfg.Endpoints.Overrides["https://s3.example"] = "http://minio:9000"
	cfg.ConditionalDeleteEndpoints = map[string]bool{"https://s3.example": true}
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	want := cfg.WithDefaults().Clone()
	app := New(cfg)
	defer app.Close()
	cfg.Origin = "https://changed.example"
	cfg.Endpoints.Allowed["https://extra.example"] = true
	cfg.Endpoints.Overrides["https://s3.example"] = "http://changed:9000"
	cfg.ConditionalDeleteEndpoints["https://s3.example"] = false
	cfg.TrustedProxies[0] = netip.MustParsePrefix("0.0.0.0/0")
	got := app.Config()
	got.Origin = "changed"
	delete(got.Endpoints.Allowed, "https://s3.example")
	got.Endpoints.Overrides["https://s3.example"] = "http://other:9000"
	got.ConditionalDeleteEndpoints["https://s3.example"] = false
	got.TrustedProxies[0] = netip.MustParsePrefix("0.0.0.0/0")
	if actual := app.Config(); !reflect.DeepEqual(actual, want) {
		t.Fatal("runtime security settings changed through aliases", actual, want)
	}
	var jobs sync.WaitGroup
	for range 50 {
		jobs.Go(func() {
			copy := app.Config()
			copy.Endpoints.Allowed["local edit"] = true
			copy.Endpoints.Overrides["local edit"] = "modified"
			copy.ConditionalDeleteEndpoints["local edit"] = true
			copy.TrustedProxies[0] = netip.MustParsePrefix("127.0.0.0/8")
			if !reflect.DeepEqual(app.Config(), want) {
				t.Error("concurrent snapshots contaminated runtime")
			}
		})
	}
	jobs.Wait()
	if field, ok := reflect.TypeFor[State]().FieldByName("config"); !ok || field.IsExported() {
		t.Fatal("configuration owner is public")
	}
	if _, ok := reflect.TypeFor[State]().FieldByName("Config"); ok {
		t.Fatal("public configuration field returned")
	}
}

func TestRuntimeConfigDefaultsAndNilCollections(t *testing.T) {
	app := New(config.Config{})
	defer app.Close()
	got := app.Config()
	if got.Endpoints.Allowed != nil || got.Endpoints.Overrides != nil || got.TrustedProxies != nil || got.ConditionalDeleteEndpoints != nil {
		t.Fatal("nil collections changed shape")
	}
	if got.SessionTTL <= 0 || got.TemporaryBytes <= 0 || got.Limits.Transfers <= 0 {
		t.Fatal("defaults lost", got)
	}
}
