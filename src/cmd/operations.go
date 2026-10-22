package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operatoraccess"
)

var errOperationsCommand = errors.New("invalid operations command; use: operations status --url <API URL> with API_OPERATOR_TOKEN")
var errOperatorRequest = errors.New("operator status request failed; check API availability and operator credential")

func newOperationsCommand() *cobra.Command {
	command := &cobra.Command{Use: "operations", Short: "Inspect background processing on a running API", Args: noCommandArguments(errOperationsCommand), RunE: func(*cobra.Command, []string) error { return errOperationsCommand }}
	command.SetFlagErrorFunc(func(*cobra.Command, error) error { return errOperationsCommand })
	address := "http://127.0.0.1:8081"
	status := &cobra.Command{Use: "status", Short: "Read shared queue counts and this API process worker status", Args: noCommandArguments(errOperationsCommand), RunE: func(c *cobra.Command, _ []string) error {
		token, err := config.LoadOperatorToken()
		if err != nil {
			return err
		}
		return requestOperatorStatus(c.Context(), address, token, c.OutOrStdout())
	}}
	status.Flags().StringVar(&address, "url", address, "API origin; HTTPS is required except for loopback HTTP")
	command.AddCommand(status)
	return command
}

func operatorURL(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" || u.Scheme != "http" && u.Scheme != "https" {
		return nil, errOperationsCommand
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, errOperationsCommand
	}
	u.Path = "/api/operator/operations"
	return u, nil
}

func requestOperatorStatus(parent context.Context, address, token string, output io.Writer) error {
	endpoint, err := operatorURL(address)
	if err != nil || !operatoraccess.ValidToken(token) {
		return errOperationsCommand
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errOperationsCommand
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 2}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errOperatorRequest
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errOperatorRequest
	}
	const limit = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) > limit {
		return errOperatorRequest
	}
	var status operations.OperatorStatus
	if json.Unmarshal(raw, &status) != nil || !operations.ValidStatus(status) {
		return errOperatorRequest
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if encoder.Encode(status) != nil {
		return errOperatorRequest
	}
	return nil
}
