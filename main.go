package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"github.com/StephenBrown2/mermaidgen/flowchart"
	"github.com/andygrunwald/go-jira"
)

type AuthCreds struct {
	Username string
	Token    string
}

var ErrIncompleteCredentials = errors.New("must provide 'username' and 'token' keys in file")

func getAuthCreds() (creds AuthCreds, err error) {
	fileName := os.ExpandEnv("${HOME}/.config/gojira")

	file, err := ioutil.ReadFile(fileName)
	if err != nil {
		return creds, fmt.Errorf("reading file: %w", err)
	}

	if err = json.Unmarshal(file, &creds); err != nil {
		return creds, fmt.Errorf("unmarshalling file: %w", err)
	}

	if creds.Username == "" || creds.Token == "" {
		return creds, fmt.Errorf("%w: %q", ErrIncompleteCredentials, fileName)
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
	return fmt.Sprintf("%s -- %s --> %s", l.From.Key, l.Link.Type.Name, l.To.Key)
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
		style.More = `color:#fff`
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
		style.More = `color:#fff`
	case "Done":
		style.Fill = `#008000`
		style.Stroke = flowchart.ColorGreen
		style.More = `color:#0f0`
	}

	return style
}

func AddJiraNode(fc *flowchart.Flowchart, issue *jira.Issue) (node *flowchart.Node) {
	node = fc.GetNode(issue.Key)
	if node == nil {
		node = fc.AddNode(issue.Key)
		text := issue.Fields.Summary
		status := issue.Fields.Status.Name
		node.Style = GetStatusStyle(fc, status)
		node.Link = "https://jumpcloud.atlassian.net/browse/" + issue.Key
		node.LinkText = "Jira: " + issue.Key
		node.AddLines(fmt.Sprintf("%s - %s", issue.Key, status), strings.ReplaceAll(html.EscapeString(text), "&#", "#"))
	}

	return node
}

func AddLink(fc *flowchart.Flowchart, link JiraLink) {
	n1 := AddJiraNode(fc, link.From)
	n2 := AddJiraNode(fc, link.To)
	e := fc.AddEdge(n1, n2)
	e.Text = []string{link.Link.Type.Outward}

	if strings.EqualFold(link.Link.Type.Name, "relates") {
		e.Shape = flowchart.EShapeLine
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

	getAllLinks(issue, c, linkSet, fc)

	return nil
}

func promptForIssue() string {
	fmt.Print("Issue Number: ")

	r := bufio.NewReader(os.Stdin)

	issueNum, err := r.ReadString('\n')
	if err != nil {
		fmt.Printf("Error getting issue number: %v\n", err)
		os.Exit(1)
	}

	return issueNum
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

	jiraClient, err := jira.NewClient(tp.Client(), "https://jumpcloud.atlassian.net/")
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
			fmt.Printf("\n\n```mermaid\n%s```\n\n", flow.String())
			fmt.Println(flow.LiveURL())
		} else {
			fmt.Println("None")
		}
	}

	os.Exit(returnCode)
}
