package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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

type Link struct {
	From      string
	FromTitle string
	Type      string
	Text      string
	To        string
	ToTitle   string
}

func (l Link) String() string {
	return fmt.Sprintf("%s -- %s --> %s", l.From, l.Type, l.To)
}

func AddJiraNode(fc *flowchart.Flowchart, key, text string) (node *flowchart.Node) {
	node = fc.GetNode(key)
	if node == nil {
		node = fc.AddNode(key)
		node.AddLines(key, text)
		node.Link = "https://jumpcloud.atlassian.net/browse/" + key
		node.LinkText = "Jira: " + key
	}

	return node
}

func AddLink(fc *flowchart.Flowchart, link Link) {
	n1 := AddJiraNode(fc, link.From, link.FromTitle)
	n2 := AddJiraNode(fc, link.To, link.ToTitle)
	e := fc.AddEdge(n1, n2)
	e.Text = []string{link.Text}

	if strings.EqualFold(link.Type, "relates") {
		e.Shape = flowchart.EShapeLine
	}
}

func getAllLinks(issue *jira.Issue, client *jira.Client, links StringSet, fc *flowchart.Flowchart) {
	if len(issue.Fields.IssueLinks) == 0 {
		issue, _, _ = client.Issue.Get(issue.Key, nil)
	}

	for _, link := range issue.Fields.IssueLinks {
		if link.OutwardIssue != nil {
			lo := Link{
				From: issue.Key, FromTitle: issue.Fields.Summary,
				Type: link.Type.Name, Text: link.Type.Outward,
				To: link.OutwardIssue.Key, ToTitle: link.OutwardIssue.Fields.Summary,
			}
			if !links.Exists(lo.String()) {
				AddLink(fc, lo)
				links.Add(lo.String())
				getAllLinks(link.OutwardIssue, client, links, fc)
			}
		}

		if link.InwardIssue != nil {
			li := Link{
				From: link.InwardIssue.Key, FromTitle: link.InwardIssue.Fields.Summary,
				Type: link.Type.Name, Text: link.Type.Outward,
				To: issue.Key, ToTitle: issue.Fields.Summary,
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
	fmt.Printf("Links:\n")

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

		fmt.Printf("\n```mermaid\n%s```\n\n", flow.String())
		fmt.Println(flow.LiveURL())
	}

	os.Exit(returnCode)
}
