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

func (s StringSet) List() (result []string) {
	for k := range s {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

var issueKeys = StringSet{}

type Link struct {
	From string
	Type string
	Text string
	To   string
}

func (l Link) String() string {
	return fmt.Sprintf("%s -- %s --> %s", l.From, l.Type, l.To)
}

func (l Link) StringText() string {
	if l.Type == "Relates" {
		return fmt.Sprintf("%s -- %s --- %s", l.From, l.Text, l.To)
	}
	return fmt.Sprintf("%s -- %s --> %s", l.From, l.Text, l.To)
}

func getAllLinks(issue *jira.Issue, client *jira.Client, links map[string]struct{}) {
	issueKeys.Add(issue.Key)
	if len(issue.Fields.IssueLinks) == 0 {
		issue, _, _ = client.Issue.Get(issue.Key, nil)
	}
	for _, link := range issue.Fields.IssueLinks {
		if link.OutwardIssue != nil {
			issueKeys.Add(link.OutwardIssue.Key)
			lo := Link{From: issue.Key, Type: link.Type.Name, Text: link.Type.Outward, To: link.OutwardIssue.Key}
			if _, ok := links[lo.String()]; !ok {
				fmt.Printf("  %s\n", lo.StringText())
				links[lo.String()] = struct{}{}
				getAllLinks(link.OutwardIssue, client, links)
			}
		}
		if link.InwardIssue != nil {
			issueKeys.Add(link.InwardIssue.Key)
			li := Link{From: link.InwardIssue.Key, Type: link.Type.Name, Text: link.Type.Outward, To: issue.Key}
			if _, ok := links[li.String()]; !ok {
				fmt.Printf("  %s\n", li.StringText())
				links[li.String()] = struct{}{}
				getAllLinks(link.InwardIssue, client, links)
			}
		}
	}
}

func printIssueLinks(c *jira.Client, issueNum string) error {
	links := map[string]struct{}{}
	issue, _, err := c.Issue.Get(strings.TrimSpace(issueNum), nil)
	if err != nil {
		return fmt.Errorf("error getting issue: %w", err)
	}

	fmt.Printf("%s: %+v\n", issue.Key, issue.Fields.Summary)
	fmt.Printf("Type: %s\n", issue.Fields.Type.Name)
	fmt.Printf("Priority: %s\n", issue.Fields.Priority.Name)
	fmt.Printf("Links:\n")
	fmt.Println("```mermaid\ngraph TD")
	getAllLinks(issue, c, links)
	fmt.Println()
	for _, k := range issueKeys.List() {
		fmt.Printf("  click %s \"https://jumpcloud.atlassian.net/browse/%s\"\n", k, k)
	}
	fmt.Println("```")
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
		err := printIssueLinks(jiraClient, issueNum)
		if err != nil {
			fmt.Println(err)
			returnCode++
		}
	}
	os.Exit(returnCode)
}
