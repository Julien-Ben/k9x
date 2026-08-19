// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"strings"
	"testing"

	"github.com/derailed/k9s/internal/client"
	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func newStr(s string) *string {
	return &s
}

func TestComputeShellArgs(t *testing.T) {
	uu := map[string]struct {
		fqn, co, os string
		cfg         *genericclioptions.ConfigFlags
		e           string
	}{
		"config": {
			fqn: "fred/blee",
			co:  "c1",
			os:  "darwin",
			cfg: &genericclioptions.ConfigFlags{
				KubeConfig: newStr("coolConfig"),
			},
			e: "exec -it -n fred blee -c c1 -- sh -c " + shellCheck,
		},

		"no-config": {
			fqn: "fred/blee",
			co:  "c1",
			os:  "linux",
			e:   "exec -it -n fred blee -c c1 -- sh -c " + shellCheck,
		},

		"empty-config": {
			fqn: "fred/blee",
			cfg: new(genericclioptions.ConfigFlags),
			e:   "exec -it -n fred blee -- sh -c " + shellCheck,
		},

		"single-container": {
			fqn: "fred/blee",
			os:  "linux",
			cfg: new(genericclioptions.ConfigFlags),
			e:   "exec -it -n fred blee -- sh -c " + shellCheck,
		},

		"windows": {
			fqn: "fred/blee",
			co:  "c1",
			os:  windowsOS,
			cfg: new(genericclioptions.ConfigFlags),
			e:   "exec -it -n fred blee -c c1 -- cmd /c " + winShellCheck,
		},

		"full": {
			fqn: "fred/blee",
			co:  "c1",
			os:  windowsOS,
			cfg: &genericclioptions.ConfigFlags{
				KubeConfig:  newStr("coolConfig"),
				Context:     newStr("coolContext"),
				BearerToken: newStr("coolToken"),
			},
			e: "exec -it -n fred blee --token coolToken -c c1 -- cmd /c " + winShellCheck,
		},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			args := computeShellArgs(u.fqn, u.co, u.cfg, u.os)
			assert.Equal(t, u.e, strings.Join(args, " "))
		})
	}
}

func TestKubectlArgsSelectedContextWins(t *testing.T) {
	flags := &genericclioptions.ConfigFlags{
		Context:    newStr("cli-context"),
		KubeConfig: newStr("kubeconfig"),
	}
	opts := &shellOpts{
		context: "row-context",
		args:    computeShellArgs("default/pod-a", "main", flags, "linux"),
	}

	args := kubectlArgs(client.NewConfig(flags), "active-context", opts)
	joined := strings.Join(args, " ")

	assert.Equal(t, 1, strings.Count(joined, "--context"))
	assert.Contains(t, joined, "--context row-context")
	assert.NotContains(t, joined, "cli-context")
	assert.Equal(t, 1, strings.Count(joined, "--kubeconfig"))
}
