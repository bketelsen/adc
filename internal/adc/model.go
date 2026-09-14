package adc

import (
	"fmt"
	"slices"
	"strings"
)

type Organization struct{ ID, Name, Description, Execution string }
type Account struct {
	BaseURL                          string
	ID, User, Name, Provider, Secret string
	Local                            bool
	Limit                            int
}
type Agent struct {
	Provider                                                                  string
	ID, Org, Name, Description, ReportsTo, Category, Model, Effort, Authority string
	Tools                                                                     []string
}
type Assignment struct {
	Completion                                                              *CompletionPolicy
	Attention                                                               *AttentionPolicy
	AttentionStarted                                                        string
	Area                                                                    string
	Obligation                                                              string
	ConstrainCapabilities                                                   bool
	Execution                                                               string
	Capabilities                                                            []CapabilityGrant
	ExtraAccount                                                            string
	Schedule, ScheduledFor                                                  string
	ConstrainTools                                                          bool
	Tools                                                                   []string
	Kind, Proposal, Authority                                               string
	ID, Org, Title, Prompt, Owner, Account, Creator, State, Output, Created string
	Publication                                                             bool
	Revision                                                                int
}
type Run struct {
	AttentionStage                                                                                                                    string
	CandidateRevision, ReviewStage                                                                                                    string
	Preflight                                                                                                                         *PreflightSpec
	Superseded                                                                                                                        bool
	Execution                                                                                                                         string
	Provider, Account                                                                                                                 string
	RequiredTools                                                                                                                     []string
	Created, LastStarted                                                                                                              string
	Activations                                                                                                                       int
	Code                                                                                                                              []CodeEvidence
	Steering, UpdatesPending                                                                                                          bool
	ID, Org, Task, Agent, Parent, Title, Prompt, Category, Model, Family, Authority, State, Result, Error, Session, Workspace, NextAt string
	Tools                                                                                                                             []string
	Attempts, Turns, ReviewRounds, Reassignments                                                                                      int
	ReviewOf, ReviewedRevision                                                                                                        string
}
type Document struct {
	ID, Org, Task, Run, Title, Content, Source, Created string
	Revision                                            int
}
type Connection struct {
	ID, Org, Name, Transport, Command, URL string
	Args                                   []string
	Env                                    map[string]string
	Headers                                map[string]string
}
type Decision struct {
	Action                                            *DecisionAction `json:",omitempty"`
	Brief, Outcome, ResolvedBy, ResolvedAt            string
	Acceptance                                        *DecisionAcceptance `json:",omitempty"`
	Document, Replaces                                string
	ID, Org, Task, Run, Question, Answer, State, Kind string
	Proposal                                          []Agent
}
type Review struct {
	Stage                                                                  string
	ID, Org, Task, Run, Target, Revision, Model, Family, Verdict, Findings string
}
type Model struct{ ID, Name, Family string }

func Family(model string) string {
	name := strings.ToLower(model)
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	for prefix, family := range map[string]string{"gpt-": "openai-gpt", "claude-": "anthropic-claude", "grok-": "xai-grok", "gemini-": "google-gemini", "qwen": "alibaba-qwen", "deepseek": "deepseek", "llama": "meta-llama", "gemma": "google-gemma", "mistral": "mistral", "mixtral": "mistral"} {
		if strings.HasPrefix(name, prefix) {
			return family
		}
	}
	return ""
}

func authorityRank(a string) int {
	switch a {
	case "observe":
		return 0
	case "draft":
		return 1
	case "approved-action":
		return 2
	}
	return -1
}
func NarrowAuthority(parent, child string) (string, error) {
	p, c := authorityRank(parent), authorityRank(child)
	if p < 0 || c < 0 {
		return "", fmt.Errorf("unknown autonomy policy")
	}
	if c > p {
		return parent, nil
	}
	return child, nil
}
func Subset(child, parent []string) bool {
	for _, v := range child {
		if !slices.Contains(parent, v) {
			return false
		}
	}
	return true
}
func CanReview(implementation, review string) error {
	a, b := Family(implementation), Family(review)
	if a == "" || b == "" {
		return fmt.Errorf("model family must be explicitly recognized")
	}
	if a == b {
		return fmt.Errorf("independent review requires a different model family")
	}
	return nil
}
