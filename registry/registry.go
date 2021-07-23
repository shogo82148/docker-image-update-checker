package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const dockerHubHost = "registry-1.docker.io"

// Client is a minimum implementation of Docker registry Client.
type Client struct {
	client *http.Client

	mu        sync.RWMutex
	tokens    map[string]*registryToken
	loginInfo map[string]*loginInfo
}

type Manifests struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`

	// application/vnd.docker.distribution.manifest.list.v2+json
	Manifests []*Manifest `json:"manifests,omitempty"`

	// application/vnd.docker.distribution.manifest.v2+json
	Config *Config  `json:"config,omitempty"`
	Layers []*Layer `json:"layers,omitempty"`
}

type Manifest struct {
	Digest    string    `json:"digest"`
	MediaType string    `json:"mediaType"`
	Platform  *Platform `json:"platform"`
	Size      int64     `json:"size"`
}

type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

type Config struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type Layer struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type loginInfo struct {
	username string
	password string
}

type registryToken struct {
	mu        sync.RWMutex
	token     string
	updatedAt time.Time
}

type registryError struct {
	statusCode int
	header     http.Header
}

func (err *registryError) Error() string {
	return fmt.Sprintf("unexpected status code: %d", err.statusCode)
}

func New() *Client {
	return &Client{
		client: &http.Client{},
	}
}

// Login logins to the Docker registry.
func (c *Client) Login(ctx context.Context, host, username, password string) error {
	host = strings.ToLower(host)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loginInfo == nil {
		c.loginInfo = make(map[string]*loginInfo)
	}
	c.loginInfo[host] = &loginInfo{
		username: username,
		password: password,
	}
	return nil
}

// get a new authentication token
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
		return "", &registryError{
			statusCode: resp.StatusCode,
			header:     resp.Header,
		}
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

func (c *Client) refreshToken(ctx context.Context, host, endpoint, service, scope string) (string, error) {
	lastUpdatedAt := time.Now()
	host = strings.ToLower(host)

	c.mu.Lock()
	if c.tokens == nil {
		c.tokens = make(map[string]*registryToken)
	}
	token := c.tokens[host]
	if token == nil {
		token = &registryToken{}
		c.tokens[host] = token
	}
	c.mu.Unlock()

	token.mu.Lock()
	defer token.mu.Unlock()
	if token.updatedAt.After(lastUpdatedAt) {
		return token.token, nil
	}

	newToken, err := c.getToken(ctx, endpoint, service, scope)
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}
	token.token = newToken
	token.updatedAt = time.Now()
	return newToken, nil
}

func (c *Client) getCachedToken(host string) string {
	host = strings.ToLower(host)

	c.mu.RLock()
	if c.tokens == nil {
		c.mu.RUnlock()
		return ""
	}
	token := c.tokens[host]
	c.mu.RUnlock()

	if token == nil {
		return ""
	}
	token.mu.RLock()
	defer token.mu.RUnlock()
	return token.token
}

func (c *Client) getManifests(ctx context.Context, host, repo, tag string) (*Manifests, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json;q=0.9")
	if token := c.getCachedToken(host); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &registryError{
			statusCode: resp.StatusCode,
			header:     resp.Header,
		}
	}

	dec := json.NewDecoder(resp.Body)
	var manifests *Manifests
	if err := dec.Decode(&manifests); err != nil {
		return nil, err
	}
	return manifests, nil
}

func (c *Client) GetManifests(ctx context.Context, image string) (*Manifests, error) {
	host, repo, tag := GetRepository(image)

	var manifests *Manifests
	var err error
	if manifests, err = c.getManifests(ctx, host, repo, tag); err == nil {
		return manifests, nil
	}

	var repoErr *registryError
	if !errors.As(err, &repoErr) {
		return nil, err
	}
	if repoErr.statusCode != http.StatusUnauthorized {
		return nil, err
	}

	h := repoErr.header.Get("Www-Authenticate")
	if h != "" {
		params, err := parseWWWAuthenticate(h)
		if err != nil {
			return nil, err
		}
		_, err = c.refreshToken(ctx, host, params["realm"], params["service"], params["scope"])
		if err != nil {
			return nil, err
		}
	}

	return c.getManifests(ctx, host, repo, tag)
}

// GetRepository splits the image name to host, repository, and tag.
func GetRepository(image string) (host, repo, tag string) {
	if idx := strings.IndexRune(image, ':'); idx >= 0 {
		tag = image[idx+1:]
		image = image[:idx]
	} else {
		tag = "latest"
	}

	if idx := strings.IndexRune(image, '/'); idx >= 0 {
		if strings.ContainsRune(image[:idx], '.') {
			// Docker registry v2 API
			host = image[:idx]
			repo = image[idx+1:]
		} else {
			// Third party image on DockerHub
			host = dockerHubHost
			repo = image
		}
	} else {
		// Official Image on DockerHub
		host = dockerHubHost
		repo = "library/" + image
	}
	return
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
