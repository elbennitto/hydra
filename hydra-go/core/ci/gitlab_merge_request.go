package ci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/core/git"
)

var (
	ensurePromoteMergeRequestHook func(repo *git.Repo, entry PromotionEntry, cfg *Config) error
	gitlabHTTPClient              = http.DefaultClient
)

func ensurePromoteMergeRequest(repo *git.Repo, entry PromotionEntry, cfg *Config) error {
	if ensurePromoteMergeRequestHook != nil {
		return ensurePromoteMergeRequestHook(repo, entry, cfg)
	}

	targetBranch := resolveMergeRequestTargetBranch(repo, cfg.CI.UpstreamBranch)
	client, ok, err := newGitLabClientFromEnv(repo)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("GitLab merge request client unavailable")
	}

	exists, err := client.mergeRequestExists(entry.Branch, targetBranch)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	return client.createMergeRequest(entry.Branch, targetBranch, entry.CommitMessage())
}

type gitLabClient struct {
	baseURL     string
	projectID   string
	token       string
	tokenHeader string
	httpClient  *http.Client
}

func newGitLabClientFromEnv(repo *git.Repo) (*gitLabClient, bool, error) {
	baseURL, err := gitLabBaseURL(repo)
	if err != nil {
		return nil, false, fmt.Errorf("resolve GitLab API base URL: %w", err)
	}

	projectID, err := gitLabProjectID(repo)
	if err != nil {
		return nil, false, fmt.Errorf("resolve GitLab project id/path: %w", err)
	}

	token, header, err := gitLabToken(repo)
	if err != nil {
		return nil, false, err
	}

	client := gitlabHTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	return &gitLabClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		projectID:   url.PathEscape(projectID),
		token:       token,
		tokenHeader: header,
		httpClient:  client,
	}, true, nil
}

func gitLabBaseURL(repo *git.Repo) (string, error) {
	if serverURL := strings.TrimSpace(os.Getenv("CI_SERVER_URL")); serverURL != "" {
		return strings.TrimRight(serverURL, "/") + "/api/v4", nil
	}

	remoteURL, err := repo.RemoteURL()
	if err != nil {
		return "", err
	}

	host, err := parseGitRemoteHost(remoteURL)
	if err != nil {
		return "", err
	}
	return "https://" + host + "/api/v4", nil
}

func gitLabProjectID(repo *git.Repo) (string, error) {
	if projectID := strings.TrimSpace(os.Getenv("CI_PROJECT_ID")); projectID != "" {
		return projectID, nil
	}
	if projectPath := strings.TrimSpace(os.Getenv("CI_PROJECT_PATH")); projectPath != "" {
		return projectPath, nil
	}

	remoteURL, err := repo.RemoteURL()
	if err != nil {
		return "", err
	}

	projectPath, err := parseGitRemoteProjectPath(remoteURL)
	if err != nil {
		return "", err
	}
	return projectPath, nil
}

func gitLabToken(repo *git.Repo) (string, string, error) {
	if repo != nil {
		secretCfg, err := LoadSecretsConfig(repo.Path())
		if err != nil {
			return "", "", fmt.Errorf("load CI secrets: %w", err)
		}
		if token := strings.TrimSpace(secretCfg.Secrets.Publish.GitLabToken); token != "" {
			return token, "PRIVATE-TOKEN", nil
		}
	}
	return "", "", fmt.Errorf("missing GitLab token: set secrets.publish.gitlabToken in .hydra-ci-secrets.sops.yaml")
}

func resolveMergeRequestTargetBranch(repo *git.Repo, upstream string) string {
	branch, err := repo.ResolveUpstreamBranch(upstream)
	if err == nil && strings.TrimSpace(branch) != "" {
		return branch
	}

	trimmed := strings.TrimSpace(upstream)
	switch trimmed {
	case "", "origin/HEAD", "refs/remotes/origin/HEAD":
		return "main"
	}
	trimmed = strings.TrimPrefix(trimmed, "refs/remotes/")
	if idx := strings.Index(trimmed, "/"); idx >= 0 && idx+1 < len(trimmed) {
		return trimmed[idx+1:]
	}
	return trimmed
}

func (c *gitLabClient) mergeRequestExists(sourceBranch, targetBranch string) (bool, error) {
	query := url.Values{}
	query.Set("source_branch", sourceBranch)
	query.Set("target_branch", targetBranch)
	query.Set("state", "opened")

	var mergeRequests []struct {
		IID int `json:"iid"`
	}
	if err := c.doJSON(http.MethodGet, "/projects/"+c.projectID+"/merge_requests?"+query.Encode(), nil, &mergeRequests); err != nil {
		return false, fmt.Errorf("list GitLab merge requests for branch %q: %w", sourceBranch, err)
	}
	return len(mergeRequests) > 0, nil
}

func (c *gitLabClient) createMergeRequest(sourceBranch, targetBranch, title string) error {
	payload := map[string]string{
		"source_branch": sourceBranch,
		"target_branch": targetBranch,
		"title":         title,
	}
	if err := c.doJSON(http.MethodPost, "/projects/"+c.projectID+"/merge_requests", payload, nil); err != nil {
		return fmt.Errorf("create GitLab merge request for branch %q: %w", sourceBranch, err)
	}
	return nil
}

func (c *gitLabClient) doJSON(method, requestPath string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, c.baseURL+requestPath, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set(c.tokenHeader, c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		detail := strings.TrimSpace(string(msg))
		if detail == "" {
			return fmt.Errorf("unexpected status %s", resp.Status)
		}
		return fmt.Errorf("unexpected status %s: %s", resp.Status, detail)
	}

	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func parseGitRemoteHost(remoteURL string) (string, error) {
	switch {
	case strings.HasPrefix(remoteURL, "http://") || strings.HasPrefix(remoteURL, "https://"):
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return "", fmt.Errorf("parse remote URL %q: %w", remoteURL, err)
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("parse remote URL %q: missing host", remoteURL)
		}
		return parsed.Host, nil
	case strings.HasPrefix(remoteURL, "ssh://"):
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return "", fmt.Errorf("parse remote URL %q: %w", remoteURL, err)
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("parse remote URL %q: missing host", remoteURL)
		}
		return parsed.Host, nil
	default:
		at := strings.Index(remoteURL, "@")
		colon := strings.Index(remoteURL, ":")
		if at < 0 || colon < 0 || colon < at {
			return "", fmt.Errorf("unsupported remote URL %q", remoteURL)
		}
		return remoteURL[at+1 : colon], nil
	}
}

func parseGitRemoteProjectPath(remoteURL string) (string, error) {
	var rawPath string
	switch {
	case strings.HasPrefix(remoteURL, "http://") || strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "ssh://"):
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return "", fmt.Errorf("parse remote URL %q: %w", remoteURL, err)
		}
		rawPath = parsed.Path
	default:
		colon := strings.Index(remoteURL, ":")
		if colon < 0 {
			return "", fmt.Errorf("unsupported remote URL %q", remoteURL)
		}
		rawPath = remoteURL[colon+1:]
	}

	rawPath = strings.TrimPrefix(rawPath, "/")
	rawPath = strings.TrimSuffix(rawPath, ".git")
	rawPath = path.Clean(rawPath)
	rawPath = strings.TrimPrefix(rawPath, "/")
	if rawPath == "" || rawPath == "." {
		return "", fmt.Errorf("remote URL %q does not contain a project path", remoteURL)
	}
	return rawPath, nil
}
