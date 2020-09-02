package config

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	common "github.com/ncabatoff/process-exporter"
	"gopkg.in/yaml.v2"
)

type (
	Matcher interface {
		// Match returns empty string for no match, or the group name on success.
		Match(common.ProcAttributes) bool
	}

	FirstMatcher struct {
		matchers []common.MatchNamer
	}

	commMatcher struct {
		comms map[string]struct{}
	}

	exeMatcher struct {
		exes map[string]string
	}

	cmdlineMatcher struct {
		regexes  []*regexp.Regexp
		captures map[string]string
	}

	andMatcher []Matcher

	templateNamer struct {
		template *template.Template
	}

	matchNamer struct {
		andMatcher
		templateNamer
	}

	templateParams struct {
		Comm      string
		ExeBase   string
		ExeFull   string
		Username  string
		PID       int
		StartTime time.Time
		Matches   map[string]string
	}
)

func (c *cmdlineMatcher) String() string {
	return fmt.Sprintf("cmdlines: %+v", c.regexes)
}

func (e *exeMatcher) String() string {
	return fmt.Sprintf("exes: %+v", e.exes)
}

func (c *commMatcher) String() string {
	var comms = make([]string, 0, len(c.comms))
	for cm := range c.comms {
		comms = append(comms, cm)
	}
	return fmt.Sprintf("comms: %+v", comms)
}

func (f FirstMatcher) String() string {
	return fmt.Sprintf("%v", f.matchers)
}

func (f FirstMatcher) MatchAndName(nacl common.ProcAttributes) (bool, string) {
	for _, m := range f.matchers {
		if matched, name := m.MatchAndName(nacl); matched {
			return true, name
		}
	}
	return false, ""
}

func (m *matchNamer) String() string {
	return fmt.Sprintf("%+v", m.andMatcher)
}

func (m *matchNamer) MatchAndName(nacl common.ProcAttributes) (bool, string) {
	if !m.Match(nacl) {
		return false, ""
	}

	matches := make(map[string]string)
	for _, m := range m.andMatcher {
		if mc, ok := m.(*cmdlineMatcher); ok {
			for k, v := range mc.captures {
				matches[k] = v
			}
		}
	}

	exebase, exefull := nacl.Name, nacl.Name
	if len(nacl.Cmdline) > 0 {
		exefull = nacl.Cmdline[0]
		exebase = filepath.Base(exefull)
	}

	var buf bytes.Buffer
	m.template.Execute(&buf, &templateParams{
		Comm:      nacl.Name,
		ExeBase:   exebase,
		ExeFull:   exefull,
		Matches:   matches,
		Username:  nacl.Username,
		PID:       nacl.PID,
		StartTime: nacl.StartTime,
	})
	return true, buf.String()
}

func (m *commMatcher) Match(nacl common.ProcAttributes) bool {
	_, found := m.comms[nacl.Name]
	return found
}

func (m *exeMatcher) Match(nacl common.ProcAttributes) bool {
	if len(nacl.Cmdline) == 0 {
		return false
	}
	thisbase := filepath.Base(nacl.Cmdline[0])
	fqpath, found := m.exes[thisbase]
	if !found {
		return false
	}
	if fqpath == "" {
		return true
	}

	return fqpath == nacl.Cmdline[0]
}

func (m *cmdlineMatcher) Match(nacl common.ProcAttributes) bool {
	for _, regex := range m.regexes {
		captures := regex.FindStringSubmatch(strings.Join(nacl.Cmdline, " "))
		if m.captures == nil {
			return false
		}
		subexpNames := regex.SubexpNames()
		if len(subexpNames) != len(captures) {
			return false
		}

		for i, name := range subexpNames {
			m.captures[name] = captures[i]
		}
	}
	return true
}

func (m andMatcher) Match(nacl common.ProcAttributes) bool {
	for _, matcher := range m {
		if !matcher.Match(nacl) {
			return false
		}
	}
	return true
}

type Config struct {
	MatchNamers FirstMatcher
}

func (c *Config) UnmarshalYAML(unmarshal func(v interface{}) error) error {
	type (
		root struct {
			Matchers MatcherRules `yaml:"process_names"`
		}
	)

	var r root
	if err := unmarshal(&r); err != nil {
		return err
	}

	cfg, err := r.Matchers.ToConfig()
	if err != nil {
		return err
	}
	*c = *cfg
	return nil
}

type MatcherGroup struct {
	Name         string   `yaml:"name"`
	CommRules    []string `yaml:"comm"`
	ExeRules     []string `yaml:"exe"`
	CmdlineRules []string `yaml:"cmdline"`
}

type MatcherRules []MatcherGroup

func (r MatcherRules) ToConfig() (*Config, error) {
	var cfg Config

	for _, matcher := range r {
		var matchers andMatcher

		if matcher.CommRules != nil {
			comms := make(map[string]struct{})
			for _, c := range matcher.CommRules {
				comms[c] = struct{}{}
			}
			matchers = append(matchers, &commMatcher{comms})
		}
		if matcher.ExeRules != nil {
			exes := make(map[string]string)
			for _, e := range matcher.ExeRules {
				if strings.Contains(e, "/") {
					exes[filepath.Base(e)] = e
				} else {
					exes[e] = ""
				}
			}
			matchers = append(matchers, &exeMatcher{exes})
		}
		if matcher.CmdlineRules != nil {
			var rs []*regexp.Regexp
			for _, c := range matcher.CmdlineRules {
				r, err := regexp.Compile(c)
				if err != nil {
					return nil, fmt.Errorf("bad cmdline regex %q: %v", c, err)
				}
				rs = append(rs, r)
			}
			matchers = append(matchers, &cmdlineMatcher{
				regexes:  rs,
				captures: make(map[string]string),
			})
		}
		if len(matchers) == 0 {
			return nil, fmt.Errorf("no matchers provided")
		}

		nametmpl := matcher.Name
		if nametmpl == "" {
			nametmpl = "{{.ExeBase}}"
		}
		tmpl := template.New("cmdname")
		tmpl, err := tmpl.Parse(nametmpl)
		if err != nil {
			return nil, fmt.Errorf("bad name template %q: %v", nametmpl, err)
		}

		matchNamer := &matchNamer{matchers, templateNamer{tmpl}}
		cfg.MatchNamers.matchers = append(cfg.MatchNamers.matchers, matchNamer)
	}

	return &cfg, nil
}

// ReadRecipesFile opens the named file and extracts recipes from it.
func ReadFile(cfgpath string, debug bool) (*Config, error) {
	content, err := ioutil.ReadFile(cfgpath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file %q: %v", cfgpath, err)
	}
	if debug {
		log.Printf("Config file %q contents:\n%s", cfgpath, content)
	}
	return GetConfig(string(content), debug)
}

// GetConfig extracts Config from content by parsing it as YAML.
func GetConfig(content string, debug bool) (*Config, error) {
	var cfg Config
	err := yaml.Unmarshal([]byte(content), &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
