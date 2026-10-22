package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func TestOperatorURLRejectsCredentialLeaksAndPlainRemoteHTTP(t *testing.T) {
	for _, address := range []string{"http://example.test", "https://user:private@example.test", "https://example.test/?token=private", "https://example.test/#private", "https://example.test/path", "http://127.0.0.1/?", "ftp://localhost", "https://example.test/%2f"} {
		if _, err := operatorURL(address); err != errOperationsCommand {
			t.Fatal("unsafe operator URL accepted", address)
		}
	}
	for _, address := range []string{"http://localhost:8081", "http://127.0.0.1:8081", "http://[::1]:8081", "https://example.test"} {
		if _, err := operatorURL(address); err != nil {
			t.Fatal("valid operator URL rejected", err)
		}
	}
}

func TestOperatorCommandRequestsLiveStatusAndBoundsResponse(t *testing.T) {
	secret := strings.Repeat("operator-", 8)
	response := operations.OperatorStatus{CheckedAt: time.Now(), Queue: operations.QueueSummary{Scope: "store", Queued: 3}, Worker: operations.WorkerStatus{Scope: "process", Running: true, Slots: 2}}
	mode := "success"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/operator/operations" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("wrong operator request")
		}
		switch mode {
		case "redirect":
			w.Header().Set("Location", "/private-leak")
			w.WriteHeader(http.StatusFound)
		case "large":
			_, _ = io.WriteString(w, strings.Repeat("x", (64<<10)+1))
		case "error":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "private-secret-upstream-error")
		default:
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()
	for _, name := range []string{"success", "redirect", "large", "error"} {
		mode = name
		var output bytes.Buffer
		err := requestOperatorStatus(context.Background(), server.URL, secret, &output)
		if name == "success" {
			if err != nil || !strings.Contains(output.String(), `"queued": 3`) || strings.Contains(output.String(), secret) {
				t.Fatal(output.String(), err)
			}
		} else if err != errOperatorRequest || output.Len() != 0 || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe failure", output.String(), err)
		}
	}
}

func TestOperationsHelpDoesNotReadConfigurationOrStartAPI(t *testing.T) {
	t.Setenv("API_DB_DRIVER", "invalid")
	t.Setenv("API_OPERATOR_TOKEN", "invalid")
	for _, args := range [][]string{{"--help"}, {"status", "--help"}} {
		var output bytes.Buffer
		command := newOperationsCommand()
		command.SetOut(&output)
		command.SetArgs(args)
		if err := command.Execute(); err != nil || !strings.Contains(output.String(), "Usage:") {
			t.Fatal("help failed", err)
		}
	}
}

func TestRootOperatorStatusDoesNotOpenDatabase(t *testing.T) {
	secret := strings.Repeat("operator-", 8)
	t.Setenv("API_DB_DRIVER", "invalid")
	t.Setenv("API_OPERATOR_TOKEN", secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("missing operator credential")
		}
		_ = json.NewEncoder(w).Encode(operations.OperatorStatus{CheckedAt: time.Now(), Queue: operations.QueueSummary{Scope: "store"}, Worker: operations.WorkerStatus{Scope: "process"}})
	}))
	defer server.Close()
	var output bytes.Buffer
	command := newRootCommand(func() error { t.Fatal("status started API"); return nil }, &output, io.Discard)
	command.SetArgs([]string{"operations", "status", "--url", server.URL})
	if err := command.Execute(); err != nil || !strings.Contains(output.String(), `"scope": "store"`) {
		t.Fatal("live CLI read failed", err)
	}
}
