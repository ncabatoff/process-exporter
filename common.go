package common

import "fmt"

type (
	// ProcAttributes ...
	ProcAttributes struct {
		Pid      int
		Name     string
		Cmdline  []string
		Username string
		Pod      string
	}
	// MatchNamer ...
	MatchNamer interface {
		// MatchAndName returns false if the match failed, otherwise
		// true and the resulting name.
		MatchAndName(ProcAttributes) (bool, string)
		fmt.Stringer
	}

	// Resolver fills any additional fields in ProcAttributes
	Resolver interface {
		// Resolve fills any additional fields in ProcAttributes
		Resolve(*ProcAttributes)
		fmt.Stringer
	}

	// Labeler is used to add additional labels to groupname
	Labeler struct {
		showUser        bool
		k8sEnabled      bool
		podDefaultLabel bool
		resolvers       []Resolver
	}
)

// NewLabeler constructor
func NewLabeler(showUser bool, k8sEnabled bool) *Labeler {
	return &Labeler{
		showUser:   showUser,
		k8sEnabled: k8sEnabled,
		resolvers:  []Resolver{},
	}
}

// AddResolver ...
func (nmr *Labeler) AddResolver(resolver Resolver) {
	nmr.resolvers = append(nmr.resolvers, resolver)
}

// GetLabels to add to groupname...
func (nmr *Labeler) GetLabels(nacl ProcAttributes) string {
	ret := ""
	if !nmr.showUser && !nmr.k8sEnabled {
		return ret
	}
	for _, res := range nmr.resolvers {
		res.Resolve(&nacl)
	}
	if nmr.showUser {
		ret += "user:" + nacl.Username + ";"
	}
	if nmr.k8sEnabled {
		ret += "pod:" + nacl.Pod + ";"
	}
	return ret
}
