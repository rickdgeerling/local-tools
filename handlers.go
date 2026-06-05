package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
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

// handleGitHubPull handles GET /gh/pull/{id}.
func handleGitHubPull(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidID(id) {
		writeError(w, http.StatusBadRequest, "Invalid PR number")
		return
	}

	// Fetch PR details (title, body, commits)
	stdout, stderr, err := execGh(r.Context(), "pr", "view", "--json", "title,body,commits", id)
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
	diffOut, diffErrOut, err := execGh(r.Context(), "pr", "diff", id)
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
	if err := execFestival(r.Context(), req.Message); err != nil {
		fmt.Printf("festival error: %v\n", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleGitHubIssue handles GET /gh/issue/{id}.
func handleGitHubIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidID(id) {
		writeError(w, http.StatusBadRequest, "Invalid issue number")
		return
	}

	stdout, stderr, err := execGh(r.Context(), "issue", "view", "--json", "title,body,comments", id)
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

// handleSearch handles GET /search?q=... and returns a stub placeholder response.
func handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, 400, "Missing 'q' query parameter")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	fmt.Fprint(w, `{"results":[]}`)
}

// handleFetch handles POST /fetch and returns a stub placeholder response.
func handleFetch(w http.ResponseWriter, r *http.Request) {
	var req FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON body")
		return
	}
	if len(req.URLs) > 10 {
		writeError(w, 400, "Maximum 10 URLs allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	fmt.Fprint(w, `{"results":[]}`)
}
