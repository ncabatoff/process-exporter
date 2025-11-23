package config

import (
	"time"

	// "github.com/kylelemons/godebug/pretty"
	common "github.com/ncabatoff/process-exporter"
	. "gopkg.in/check.v1"
)

func (s MySuite) TestConfigBasic(c *C) {
	yml := `
process_names:
  - exe: 
    - bash
  - exe: 
    - sh
  - exe: 
    - /bin/ksh
`
	cfg, err := GetConfig(yml, false)
	c.Assert(err, IsNil)
	c.Check(cfg.MatchNamers.matchers, HasLen, 3)

	bash := common.ProcAttributes{Name: "bash", Cmdline: []string{"/bin/bash"}}
	sh := common.ProcAttributes{Name: "sh", Cmdline: []string{"sh"}}
	ksh := common.ProcAttributes{Name: "ksh", Cmdline: []string{"/bin/ksh"}}

	found, name := cfg.MatchNamers.matchers[0].MatchAndName(bash)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "bash")
	found, name = cfg.MatchNamers.matchers[0].MatchAndName(sh)
	c.Check(found, Equals, false)
	found, name = cfg.MatchNamers.matchers[0].MatchAndName(ksh)
	c.Check(found, Equals, false)

	found, name = cfg.MatchNamers.matchers[1].MatchAndName(bash)
	c.Check(found, Equals, false)
	found, name = cfg.MatchNamers.matchers[1].MatchAndName(sh)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "sh")
	found, name = cfg.MatchNamers.matchers[1].MatchAndName(ksh)
	c.Check(found, Equals, false)

	found, name = cfg.MatchNamers.matchers[2].MatchAndName(bash)
	c.Check(found, Equals, false)
	found, name = cfg.MatchNamers.matchers[2].MatchAndName(sh)
	c.Check(found, Equals, false)
	found, name = cfg.MatchNamers.matchers[2].MatchAndName(ksh)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "ksh")
}

func (s MySuite) TestConfigCgroups(c *C) {
	yml := `
process_names:
  - cgroup:
    - "/user.slice"
  - cgroup:
    - "/system.slice/docker-"
  - cgroup:
    - "/system.slice/docker-8dde.scope/"
`
	cfg, err := GetConfig(yml, false)
	c.Assert(err, IsNil)
	c.Check(cfg.MatchNamers.matchers, HasLen, 3)

	none := common.ProcAttributes{Name: "none"}
	empty := common.ProcAttributes{Name: "empty", Cgroups: []string{}}
	user := common.ProcAttributes{Name: "user", Cgroups: []string{"/user.slice/user-1000.slice/user@1000.service/app.slice/app-shell.scope"}}
	docker := common.ProcAttributes{Name: "docker", Cgroups: []string{"/system.slice/ssh.service", "/system.slice/docker-8dde.scope"}}

	// /user.slice
	found, name := cfg.MatchNamers.matchers[0].MatchAndName(none)
	c.Check(found, Equals, false)
	found, _ = cfg.MatchNamers.matchers[0].MatchAndName(empty)
	c.Check(found, Equals, false)
	found, name = cfg.MatchNamers.matchers[0].MatchAndName(user)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "user")
	found, _ = cfg.MatchNamers.matchers[0].MatchAndName(docker)
	c.Check(found, Equals, false)

	// /system.slice/docker-
	found, _ = cfg.MatchNamers.matchers[1].MatchAndName(none)
	c.Check(found, Equals, false)
	found, _ = cfg.MatchNamers.matchers[1].MatchAndName(empty)
	c.Check(found, Equals, false)
	found, _ = cfg.MatchNamers.matchers[1].MatchAndName(user)
	c.Check(found, Equals, false)
	found, name = cfg.MatchNamers.matchers[1].MatchAndName(docker)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "docker")

	// /system.slice/docker-8dde.scope/
	found, _ = cfg.MatchNamers.matchers[2].MatchAndName(none)
	c.Check(found, Equals, false)
	found, _ = cfg.MatchNamers.matchers[2].MatchAndName(empty)
	c.Check(found, Equals, false)
	found, _ = cfg.MatchNamers.matchers[2].MatchAndName(user)
	c.Check(found, Equals, false)
	found, _ = cfg.MatchNamers.matchers[2].MatchAndName(docker)
	c.Check(found, Equals, false) // No match, tailing slash
}

func (s MySuite) TestConfigTemplates(c *C) {
	yml := `
process_names:
  - exe: 
    - postmaster
    cmdline: 
    - "-D\\s+.+?(?P<Path>[^/]+)(?:$|\\s)"
    name: "{{.ExeBase}}:{{.Matches.Path}}"
  - exe: 
    - prometheus
    name: "{{.ExeFull}}:{{.PID}}"
  - comm:
    - cat
    name: "{{.StartTime}}"
`
	cfg, err := GetConfig(yml, false)
	c.Assert(err, IsNil)
	c.Check(cfg.MatchNamers.matchers, HasLen, 3)

	postgres := common.ProcAttributes{Name: "postmaster", Cmdline: []string{"/usr/bin/postmaster", "-D", "/data/pg"}}
	found, name := cfg.MatchNamers.matchers[0].MatchAndName(postgres)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "postmaster:pg")

	pm := common.ProcAttributes{
		Name:    "prometheus",
		Cmdline: []string{"/usr/local/bin/prometheus"},
		PID:     23,
	}
	found, name = cfg.MatchNamers.matchers[1].MatchAndName(pm)
	c.Check(found, Equals, true)
	c.Check(name, Equals, "/usr/local/bin/prometheus:23")

	now := time.Now()
	cat := common.ProcAttributes{
		Name:      "cat",
		Cmdline:   []string{"/bin/cat"},
		StartTime: now,
	}
	found, name = cfg.MatchNamers.matchers[2].MatchAndName(cat)
	c.Check(found, Equals, true)
	c.Check(name, Equals, now.String())
}
