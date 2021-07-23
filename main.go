package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Client struct {
	client *http.Client
	token  string
}

func main() {
	c := &Client{
		client: &http.Client{},
	}
	c.GetManifest(context.Background(), "latest")
}

func (c *Client) Login(ctx context.Context, host, user, password string) error {
	return nil
}

func (c *Client) getToken(ctx context.Context, endpoint, service, scope string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("service", service)
	q.Set("scope", scope)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected response code: %d", resp.StatusCode)
	}

	var body struct {
		Token string `json:"Token"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&body); err != nil {
		return "", err
	}
	if body.Token == "" {
		return "", errors.New("response does not contains token")
	}
	return body.Token, nil
}

func (c *Client) getManifest(ctx context.Context, tag string) error {
	url := fmt.Sprintf("https://registry-1.docker.io/v2/library/alpine/manifests/%s", tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Println(resp.StatusCode)

	h := resp.Header.Get("Www-Authenticate")
	if h != "" {
		params, err := parseWWWAuthenticate(h)
		if err != nil {
			return err
		}
		token, err := c.getToken(ctx, params["realm"], params["service"], params["scope"])
		if err != nil {
			return err
		}
		c.token = token
	}

	io.Copy(os.Stdout, resp.Body)
	return nil
}

func (c *Client) GetManifest(ctx context.Context, tag string) error {
	if err := c.getManifest(ctx, tag); err != nil {
		return err
	}
	if err := c.getManifest(ctx, tag); err != nil {
		return err
	}
	return nil
}

var partRegexp = regexp.MustCompile(`[a-zA-Z0-9_]+="[^"]*"`)

func parseWWWAuthenticate(value string) (map[string]string, error) {
	idx := strings.IndexRune(value, ' ')
	if idx < 0 {
		return nil, errors.New("authenticate type not found")
	}
	authType := value[:idx]
	if authType != "Bearer" {
		return nil, fmt.Errorf("unknown authenticate type: %s", authType)
	}

	// TODO: follow https://openid-foundation-japan.github.io/draft-ietf-oauth-v2-bearer-draft11.ja.html
	result := map[string]string{}
	params := value[idx+1:]
	for _, part := range partRegexp.FindAllString(params, -1) {
		kv := strings.SplitN(part, "=", 2)
		result[kv[0]] = kv[1][1 : len(kv[1])-1]
	}

	return result, nil
}
