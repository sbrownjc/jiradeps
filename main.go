package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Heiko-san/mermaidgen/flowchart"
	"github.com/andygrunwald/go-jira"
	"github.com/charmbracelet/huh"
)

type AuthCreds struct {
	Username string
	Token    string
	BaseURL  string
}

var baseURL string

var ErrIncompleteCredentials = errors.New("must provide 'username' and 'token' keys in file")

func getAuthCreds() (creds AuthCreds, err error) {
	fileName := os.ExpandEnv("${HOME}/.config/jiradeps.json")
	var newCreds bool
	file, err := os.ReadFile(fileName)

	usernameInput := huh.NewInput().
		Title("Username").
		Value(&creds.Username).
		Validate(func(s string) error {
			if s == "" {
				return fmt.Errorf("Username cannot be empty")
			}
			return nil
		})

	tokenInput := huh.NewInput().
		Title("API Token").
		Value(&creds.Token).
		Validate(func(s string) error {
			if s == "" {
				return fmt.Errorf("API token cannot be empty")
			}
			if len(s) < 190 {
				return fmt.Errorf("API token must be at least 190 characters")
			}
			if len(s) > 200 {
				return fmt.Errorf("API token must be at most 200 characters")
			}
			if strings.Contains(s, "\"") {
				return fmt.Errorf("API token must not contain quotes")
			}
			return nil
		})

	baseUrlInput := huh.NewInput().
		Title("Base URL").
		Value(&creds.BaseURL).
		Validate(func(s string) error {
			if s == "" {
				return fmt.Errorf("Base URL cannot be empty")
			}
			if !strings.HasSuffix(s, "/") {
				return fmt.Errorf("Base URL must end with a slash")
			}
			if !strings.HasPrefix(s, "https://") {
				return fmt.Errorf("Base URL must start with 'https://'")
			}
			return nil
		})

	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// If the file does not exist, prompt for credentials
			huh.NewForm(huh.NewGroup(baseUrlInput, usernameInput, tokenInput).Title("Jira Credentials")).Run()
			newCreds = true
		} else {
			return creds, fmt.Errorf("reading file: %w", err)
		}
	}

	if !newCreds {
		if err = json.Unmarshal(file, &creds); err != nil {
			return creds, fmt.Errorf("unmarshalling file: %w", err)
		}
	}

	if creds.Username == "" {
		usernameInput.Run()
		newCreds = true
	}
	if creds.Token == "" {
		tokenInput.Run()
		newCreds = true
	}
	if creds.BaseURL == "" {
		baseUrlInput.Run()
		newCreds = true
	}
	baseURL = creds.BaseURL

	if newCreds {
		jsonData, err := json.MarshalIndent(creds, "", "  ")
		if err != nil {
			return creds, fmt.Errorf("marshalling credentials: %w", err)
		}
		if err = os.WriteFile(fileName, jsonData, 0o600); err != nil {
			return creds, fmt.Errorf("writing file: %w", err)
		}
	}

	return creds, nil
}

type StringSet map[string]struct{}

func (s StringSet) Add(n string) {
	if _, ok := s[n]; !ok {
		s[n] = struct{}{}
	}
}

func (s StringSet) Exists(n string) bool {
	_, ok := s[n]
	return ok
}

func (s StringSet) List() (result []string) {
	for k := range s {
		result = append(result, k)
	}

	sort.Strings(result)

	return result
}

type JiraLink struct {
	From *jira.Issue
	Link *jira.IssueLink
	To   *jira.Issue
}

func (l JiraLink) String() string {
	return fmt.Sprintf("%s == %s ==> %s", l.From.Key, l.Link.Type.Name, l.To.Key)
}

func GetStatusStyle(fc *flowchart.Flowchart, status string) (style *flowchart.NodeStyle) {
	style = fc.NodeStyle(strings.ReplaceAll(status, " ", ""))

	if style.Fill != "" {
		return style
	}

	switch status {
	case "To Do":
		style.Fill = `#D3D3D3`
		style.Stroke = `#808080`
	case "In Progress":
		style.Fill = `#0052CC`
		style.Stroke = flowchart.ColorWhite
	case "In Code Review":
		style.Fill = `#998DD9`
	case "Ready for Local Testing":
		style.Fill = `#00C7E6`
	case "In Local Test":
		style.Fill = `#008DA6`
	case "Ready for Staging Test":
		style.Fill = `#FFE380`
	case "In Staging Test":
		style.Fill = `#FFAB00`
	case "Ready for Production":
		style.Fill = `#108010`
		style.Stroke = flowchart.ColorWhite
	case "Done":
		style.Fill = `#008000`
		style.Stroke = flowchart.ColorGreen
	}

	return style
}

func GetStatusIcon(status string) (icon string) {
	switch status {
	case "To Do":
		icon = "fa:fa-list"
	case "In Progress":
		icon = "fa:fa-play"
	case "In Code Review":
		icon = "fa:fa-eye"
	case "Ready for Local Testing":
		icon = "fa:fa-spinner fa:fa-laptop fa:fa-flask"
	case "In Local Test":
		icon = "fa:fa-laptop fa:fa-flask"
	case "Ready for Staging Test":
		icon = "fa:fa-spinner fa:fa-server fa:fa-flask"
	case "In Staging Test":
		icon = "fa:fa-server fa:fa-flask"
	case "Ready for Production":
		icon = "fa:fa-spinner fa:fa-server"
	case "Done":
		icon = "fa:fa-check"
	}

	return icon
}

func AddJiraNode(fc *flowchart.Flowchart, issue *jira.Issue) (node *flowchart.Node) {
	node = fc.GetNode(issue.Key)
	if node == nil {
		node = fc.AddNode(issue.Key)
		text := issue.Fields.Summary
		status := issue.Fields.Status.Name
		node.Shape = flowchart.NShapeRoundRect
		node.Style = GetStatusStyle(fc, status)
		node.Link = fmt.Sprintf("%sbrowse/%s", baseURL, issue.Key)
		node.LinkText = "Jira: " + issue.Key
		node.AddLines(
			fmt.Sprintf("%s %s - %s", GetStatusIcon(status), issue.Key, status),
			strings.ReplaceAll(html.EscapeString(text), "&#", "#"),
		)
	}

	return node
}

func AddLink(fc *flowchart.Flowchart, link JiraLink) {
	n1 := AddJiraNode(fc, link.From)
	n2 := AddJiraNode(fc, link.To)
	e := fc.AddEdge(n1, n2)
	e.Text = []string{link.Link.Type.Outward}

	e.Shape = flowchart.EShapeThickArrow

	if strings.EqualFold(link.Link.Type.Name, "relates") {
		e.Shape = flowchart.EShapeThickLine
	}
}

func getAllLinks(issue *jira.Issue, client *jira.Client, links StringSet, fc *flowchart.Flowchart) {
	if len(issue.Fields.IssueLinks) == 0 {
		issue, _, _ = client.Issue.Get(issue.Key, nil)
	}

	for _, link := range issue.Fields.IssueLinks {
		if link.OutwardIssue != nil {
			lo := JiraLink{
				From: issue,
				Link: link,
				To:   link.OutwardIssue,
			}
			if !links.Exists(lo.String()) {
				AddLink(fc, lo)
				links.Add(lo.String())
				getAllLinks(link.OutwardIssue, client, links, fc)
			}
		}

		if link.InwardIssue != nil {
			li := JiraLink{
				From: link.InwardIssue,
				Link: link,
				To:   issue,
			}
			if !links.Exists(li.String()) {
				AddLink(fc, li)
				links.Add(li.String())
				getAllLinks(link.InwardIssue, client, links, fc)
			}
		}
	}
}

// SearchWithContext performs a JQL search and returns a list of issue IDs that match the query.
// Uses the v3 API as the v2 API is deprecated, but go-jira hasn't been updated to reflect this.
// It handles pagination using the nextPageToken field.
func SearchWithContext(ctx context.Context, client *jira.Client, jql string, options *map[string]any) (issueIDs []string, err error) {
	u := url.URL{
		Path: "rest/api/3/search/jql",
	}

	body := map[string]any{
		"jql": jql,
	}

	if options != nil {
		maps.Copy(body, *options)
	}

	for {
		req, err := client.NewRequestWithContext(ctx, "POST", u.String(), &body)
		if err != nil {
			return []string{}, err
		}

		type searchResult struct {
			Issues        []jira.Issue `json:"issues" structs:"issues"`
			NextPageToken string       `json:"nextPageToken,omitempty" structs:"nextPageToken,omitempty"`
		}

		v := new(searchResult)
		resp, err := client.Do(req, v)
		if err != nil {
			err = jira.NewJiraError(resp, err)
		}

		for _, issue := range v.Issues {
			issueIDs = append(issueIDs, issue.ID)
		}

		if v.NextPageToken == "" {
			break
		}
		body["nextPageToken"] = v.NextPageToken
	}

	return issueIDs, err
}

// BulkFetchIssues fetches multiple issues by their IDs or keys using the bulkfetch endpoint.
// It retrieves specific fields for each issue to minimize data transfer.
// Uses the v3 API as the v2 API is deprecated, but go-jira hasn't been updated to reflect this.
func BulkFetchIssues(ctx context.Context, client *jira.Client, issueIDs []string) (issues []jira.Issue, err error) {
	if len(issueIDs) == 0 {
		return issues, nil
	}

	u := url.URL{
		Path: "rest/api/3/issue/bulkfetch",
	}

	body := map[string]any{
		"fields":         []string{"issuelinks", "labels", "priority", "status", "summary", "issuetype"},
		"issueIdsOrKeys": issueIDs,
	}
	req, err := client.NewRequestWithContext(ctx, "POST", u.String(), &body)
	if err != nil {
		return issues, err
	}
	type fetchResult struct {
		Issues      []jira.Issue `json:"issues" structs:"issues"`
		Expand      string       `json:"expand,omitempty" structs:"expand,omitempty"`
		IssueErrors []any        `json:"issueErrors,omitempty" structs:"issueErrors,omitempty"`
	}
	v := new(fetchResult)
	resp, err := client.Do(req, &v)
	if err != nil {
		err = jira.NewJiraError(resp, err)
	}
	return v.Issues, err
}

func SearchAndFetch(ctx context.Context, client *jira.Client, jql string, options *map[string]any) (issues []jira.Issue, err error) {
	issueIDs, err := SearchWithContext(ctx, client, jql, options)
	if err != nil {
		return nil, fmt.Errorf("error searching for issues: %w", err)
	}

	issues, err = BulkFetchIssues(ctx, client, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("error bulk fetching child issues: %w", err)
	}

	return issues, nil
}

func genDepFlowchart(c *jira.Client, issueNum string, fc *flowchart.Flowchart) error {
	linkSet := StringSet{}

	issue, _, err := c.Issue.Get(strings.TrimSpace(issueNum), nil)
	if err != nil {
		return fmt.Errorf("error getting issue: %w", err)
	}

	fmt.Printf("\n%s: %+v\n", issue.Key, issue.Fields.Summary)
	fmt.Printf("Type: %s\n", issue.Fields.Type.Name)
	fmt.Printf("Priority: %s\n", issue.Fields.Priority.Name)
	fmt.Printf("Links: ")

	if issue.Fields.Type.Name == "Epic" {
		issues, err := SearchAndFetch(context.Background(), c, fmt.Sprintf("parentEpic = %s", issue.Key), nil)
		if err != nil {
			return fmt.Errorf("error searching for child issues: %w", err)
		}
		for _, childIssue := range issues {
			getAllLinks(&childIssue, c, linkSet, fc)
		}
	} else {
		getAllLinks(issue, c, linkSet, fc)
	}

	return nil
}

func promptForIssue() (issueNum string) {
	huh.NewInput().
		Title("Issue Number").
		Value(&issueNum).Validate(func(s string) error {
		if s == "" {
			return fmt.Errorf("Issue Number cannot be empty")
		}

		// Issue Number must be in the format 'ABC-123'
		s = strings.TrimSpace(s)
		s = strings.ToUpper(s)

		// Check if it starts with letters followed by a hyphen and then digits
		if !strings.Contains(s, "-") {
			return fmt.Errorf("Issue Number must contain a hyphen")
		}

		parts := strings.Split(s, "-")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("Issue Number must contain two parts separated by a hyphen")
		}

		if !strings.ContainsFunc(parts[0], func(r rune) bool {
			return unicode.IsLetter(r)
		}) {
			return fmt.Errorf("Issue Number must start with letters")
		}

		_, err := strconv.Atoi(string(parts[1]))
		if err != nil {
			return fmt.Errorf("Issue Number must end with digits")
		}

		return nil
	}).Run()

	return issueNum
}

func imgURL(url string, format string) string {
	return strings.ReplaceAll(url, "mermaid.live/view/#pako", "mermaid.ink/img/pako") + "?type=" + format
}

func main() {
	jiraAuth, err := getAuthCreds()
	if err != nil {
		fmt.Printf("Error getting credentials: %v\n", err)
		os.Exit(1)
	}

	tp := jira.BasicAuthTransport{
		Username: jiraAuth.Username,
		Password: jiraAuth.Token,
	}

	jiraClient, err := jira.NewClient(tp.Client(), baseURL)
	if err != nil {
		fmt.Printf("Error making client: %v\n", err)
		os.Exit(1)
	}

	var issues []string
	if len(os.Args) > 1 {
		issues = os.Args[1:]
	} else {
		issues = append(issues, promptForIssue())
	}

	var returnCode int

	for _, issueNum := range issues {
		flow := flowchart.NewFlowchart()

		err := genDepFlowchart(jiraClient, issueNum, flow)
		if err != nil {
			fmt.Println(err)
			returnCode++
		}

		if len(flow.ListNodes()) > 1 {
			fmt.Printf("\n\n```mermaid\n---\nconfig:\n  theme: neutral\n---\n%s```\n\n", flow.String())
			fmt.Printf("Live: %s\n\n", flow.LiveURL())
			fmt.Printf("PNG:  %s\n\n", imgURL(flow.LiveURL(), "png"))
			fmt.Printf("SVG:  %s\n\n", imgURL(flow.LiveURL(), "svg"))
		} else {
			fmt.Println("None")
		}
	}

	os.Exit(returnCode)
}
