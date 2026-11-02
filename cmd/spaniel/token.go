package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zfogg/spaniel/internal/security"
)

func tokenSubcommand() *cobra.Command {
	var endpoint, tokenEnv string
	var asJSON bool
	cmd := &cobra.Command{Use: "token", Short: "Manage named API tokens on a running Spaniel server", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	cmd.PersistentFlags().StringVar(&endpoint, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.PersistentFlags().StringVar(&tokenEnv, "bearer-token-env", "SPANIEL_BEARER_TOKEN", "Environment variable containing an existing API token")
	cmd.PersistentFlags().BoolVar(&asJSON, "json", false, "Print JSON")
	request := func(c *cobra.Command, method, path string, body any, out any) error {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("--api must be an http(s) URL without credentials, query or fragment")
		}
		var input io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				return err
			}
			input = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(c.Context(), method, strings.TrimRight(endpoint, "/")+path, input)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if tokenEnv != "" {
			token := os.Getenv(tokenEnv)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			} else if cmd.PersistentFlags().Changed("bearer-token-env") {
				return fmt.Errorf("credential environment variable %q is empty", tokenEnv)
			}
		}
		client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connect to Spaniel: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			var failure struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&failure)
			return fmt.Errorf("HTTP %d %s: %s", resp.StatusCode, failure.Code, failure.Error)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var name, expires string
	var noExpiry bool
	create := &cobra.Command{Use: "create --name NAME", Short: "Create a token and show its secret once", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error {
		in := security.CreateInput{Name: name, NoExpiry: noExpiry}
		if expires != "" {
			at, err := time.Parse(time.RFC3339, expires)
			if err != nil {
				return fmt.Errorf("--expires-at must be RFC 3339")
			}
			in.ExpiresAt = &at
		}
		if noExpiry && expires != "" {
			return fmt.Errorf("--no-expiry and --expires-at cannot be combined")
		}
		var out struct {
			Data security.Created `json:"data"`
		}
		if err := request(c, http.MethodPost, "/api/security/tokens", in, &out); err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(c.OutOrStdout()).Encode(out.Data)
		}
		fmt.Fprintln(c.ErrOrStderr(), "Copy this API token now. It will not be shown again.")
		_, err := fmt.Fprintln(c.OutOrStdout(), out.Data.Secret)
		return err
	}}
	create.Flags().StringVar(&name, "name", "", "Required token name")
	_ = create.MarkFlagRequired("name")
	create.Flags().StringVar(&expires, "expires-at", "", "Expiry in RFC 3339 (default: one year)")
	create.Flags().BoolVar(&noExpiry, "no-expiry", false, "Create a token without expiry")
	list := &cobra.Command{Use: "list", Short: "List token metadata without secrets", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error {
		var out struct {
			Data []security.Token `json:"data"`
		}
		if err := request(c, http.MethodGet, "/api/security/tokens", nil, &out); err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(c.OutOrStdout()).Encode(out.Data)
		}
		for _, token := range out.Data {
			expiry := "never"
			if token.ExpiresAt != nil {
				expiry = token.ExpiresAt.Format(time.RFC3339)
			}
			fmt.Fprintf(c.OutOrStdout(), "%s\t%s\t%s\t%s\n", token.ID, token.Name, token.Status, expiry)
		}
		return nil
	}}
	del := &cobra.Command{Use: "delete ID", Short: "Revoke a token and its sessions", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		var out struct {
			Data map[string]bool `json:"data"`
		}
		if err := request(c, http.MethodDelete, "/api/security/tokens/"+url.PathEscape(args[0]), nil, &out); err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(c.OutOrStdout()).Encode(out.Data)
		}
		_, err := fmt.Fprintln(c.OutOrStdout(), "Token revoked.")
		return err
	}}
	cmd.AddCommand(create, list, del)
	return cmd
}
