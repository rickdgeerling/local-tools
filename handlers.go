package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// isValidID checks that id is a non-empty string of numeric digits.
func isValidID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isValidRepo checks that repo looks like "owner/name" and contains only
// characters valid in GitHub repository names.
func isValidRepo(repo string) bool {
	owner, name, found := strings.Cut(repo, "/")
	if !found || owner == "" || name == "" {
		return false
	}
	for _, c := range owner + name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// ghTarget returns the argument to pass to `gh` for the requested item. If the
// optional "repo" query parameter ("owner/name") is present, a full GitHub
// URL is constructed; otherwise the bare numeric id is returned and gh infers
// the repository from the working directory. It writes an error response and
// returns "", false on invalid input.
func ghTarget(w http.ResponseWriter, r *http.Request, invalidMsg, urlSegment string) (string, bool) {
	id := r.PathValue("id")
	if !isValidID(id) {
		writeError(w, http.StatusBadRequest, invalidMsg)
		return "", false
	}

	repo := r.URL.Query().Get("repo")
	if repo == "" {
		return id, true
	}
	if !isValidRepo(repo) {
		writeError(w, http.StatusBadRequest, "Invalid repo (expected owner/name)")
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/%s", repo, urlSegment, id), true
}

// execGh runs a gh command with a 30-second context timeout.
// It returns stdout, stderr, and any execution error.
func execGh(ctx context.Context, args ...string) (string, string, error) {
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// writeError writes a plain-text error response with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/markdown")
	w.WriteHeader(status)
	fmt.Fprint(w, msg)
}

// prJSON represents the JSON output of `gh pr view --json title,body,commits`.
type prJSON struct {
	Title   string     `json:"title"`
	Body    string     `json:"body"`
	Commits []prCommit `json:"commits"`
}

// prCommit represents a single commit in the PR JSON output.
type prCommit struct {
	MessageHeadline string `json:"messageHeadline"`
	MessageBody     string `json:"messageBody"`
}

// handleGitHubPull handles GET /gh/pull/{id}[?repo=owner/name].
func handleGitHubPull(w http.ResponseWriter, r *http.Request) {
	target, ok := ghTarget(w, r, "Invalid PR number", "pull")
	if !ok {
		return
	}

	// Fetch PR details (title, body, commits)
	stdout, stderr, err := execGh(r.Context(), "pr", "view", "--json", "title,body,commits", target)
	if err != nil {
		msg := stderr
		if msg == "" {
			msg = err.Error()
		}
		writeError(w, http.StatusInternalServerError, msg)
		return
	}

	var pr prJSON
	if err := json.Unmarshal([]byte(stdout), &pr); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to parse PR JSON: %v", err))
		return
	}

	// Fetch PR diff
	diffOut, diffErrOut, err := execGh(r.Context(), "pr", "diff", target)
	if err != nil {
		msg := diffErrOut
		if msg == "" {
			msg = err.Error()
		}
		writeError(w, http.StatusInternalServerError, msg)
		return
	}

	// Build markdown
	markdown := fmt.Sprintf("# %s\n\n%s\n\n---\nCommits\n---\n", pr.Title, pr.Body)
	for _, c := range pr.Commits {
		msg := c.MessageHeadline
		if c.MessageBody != "" {
			msg += "\n\n" + c.MessageBody
		}
		markdown += fmt.Sprintf("- %s\n", msg)
	}
	markdown += fmt.Sprintf("\n---\nDiff\n---\n%s", diffOut)

	w.Header().Set("Content-Type", "text/markdown")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, markdown)
}

// issueJSON represents the JSON output of `gh issue view --json title,body,comments`.
type issueJSON struct {
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Comments []issueComment `json:"comments"`
}

// issueComment represents a single comment in the issue JSON output.
type issueComment struct {
	Body   string      `json:"body"`
	Author issueAuthor `json:"author"`
}

// issueAuthor represents the author of an issue comment.
type issueAuthor struct {
	Login string `json:"login"`
}

// NotifyRequest represents the JSON body for POST /notify.
type NotifyRequest struct {
	Message string `json:"message"`
	Agent   string `json:"agent"`
	Level   string `json:"level"`
	Quiet   bool   `json:"quiet"`
}

var validLevels = map[string]bool{"info": true, "warning": true, "error": true}

// execFestival pipes the given message into festival --tts with a 60s timeout.
// The stdin pipe is properly closed to avoid hanging.
func execFestival(ctx context.Context, message string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "festival", "--tts")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	go func() {
		defer stdin.Close()
		io.WriteString(stdin, message)
	}()

	return cmd.Run()
}

// handleNotify handles POST /notify.
func handleNotify(w http.ResponseWriter, r *http.Request) {
	var req NotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "Missing required field 'message'")
		return
	}

	// Set defaults
	if req.Agent == "" {
		req.Agent = "OpenCode"
	}
	if req.Level == "" {
		req.Level = "info"
	} else if !validLevels[req.Level] {
		req.Level = "info"
	}

	// Determine icon for notify-send
	icon := "dialog-information"
	switch req.Level {
	case "warning":
		icon = "dialog-warning"
	case "error":
		icon = "dialog-error"
	}

	// Execute notify-send (log error but continue) with a dedicated 60s timeout
	notifyCtx, notifyCancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer notifyCancel()
	notifyCmd := exec.CommandContext(notifyCtx, "notify-send", "--icon="+icon, req.Agent, req.Message)
	if err := notifyCmd.Run(); err != nil {
		// Log but don't fail the request
		fmt.Printf("notify-send error: %v\n", err)
	}

	// Execute festival --tts (log error but continue)
	if req.Quiet != true {
		if err := execFestival(r.Context(), req.Message); err != nil {
			fmt.Printf("festival error: %v\n", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleGitHubIssue handles GET /gh/issue/{id}[?repo=owner/name].
func handleGitHubIssue(w http.ResponseWriter, r *http.Request) {
	target, ok := ghTarget(w, r, "Invalid issue number", "issues")
	if !ok {
		return
	}

	stdout, stderr, err := execGh(r.Context(), "issue", "view", "--json", "title,body,comments", target)
	if err != nil {
		msg := stderr
		if msg == "" {
			msg = err.Error()
		}
		writeError(w, http.StatusInternalServerError, msg)
		return
	}

	var issue issueJSON
	if err := json.Unmarshal([]byte(stdout), &issue); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to parse issue JSON: %v", err))
		return
	}

	markdown := fmt.Sprintf("# %s\n\n%s\n\n---\n", issue.Title, issue.Body)
	for i, c := range issue.Comments {
		if i > 0 {
			markdown += "---\n"
		}
		markdown += fmt.Sprintf("## %s said\n\n%s\n", c.Author.Login, c.Body)
	}

	w.Header().Set("Content-Type", "text/markdown")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, markdown)
}

// FetchRequest represents the JSON body for POST /fetch.
type FetchRequest struct {
	URLs []string `json:"urls"`
}

// kagiPOST posts a JSON body to the given Kagi API path and returns the raw
// response body. It reads the API token from the KAGI_TOKEN environment variable.
func kagiPOST(ctx context.Context, path string, body any) ([]byte, error) {
	token := os.Getenv("KAGI_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("KAGI_TOKEN environment variable is not set")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://kagi.com/api/v1"+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Kagi API returned %s: %s", resp.Status, string(bodyBytes))
	}
	return bodyBytes, nil
}

// writeMarkdown writes raw markdown with a 200 status.
func writeMarkdown(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/markdown")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// handleSearch handles GET /search?q=... by querying the Kagi Search API.
func handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "Missing 'q' query parameter")
		return
	}

	respBody, err := kagiPOST(r.Context(), "/search", map[string]any{
		"query":    q,
		"workflow": "search",
		"format":   "markdown",
		"limit":    10,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeMarkdown(w, respBody)
}

// handleFetch handles POST /fetch by extracting content from URLs via the Kagi Extract API.
func handleFetch(w http.ResponseWriter, r *http.Request) {
	var req FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if len(req.URLs) > 10 {
		writeError(w, http.StatusBadRequest, "Maximum 10 URLs allowed")
		return
	}
	if len(req.URLs) == 0 {
		writeMarkdown(w, nil)
		return
	}

	pages := make([]map[string]string, 0, len(req.URLs))
	for _, u := range req.URLs {
		pages = append(pages, map[string]string{"url": u})
	}

	respBody, err := kagiPOST(r.Context(), "/extract", map[string]any{
		"pages":  pages,
		"format": "markdown",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeMarkdown(w, respBody)
}
