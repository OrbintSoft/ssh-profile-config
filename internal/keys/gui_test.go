package keys

import (
	"errors"
	"testing"
)

func TestGUIAvailable(t *testing.T) {
	xsetOK := func() *fakeRunner { return newFakeRunner().on("xset", stdout("", 0)) }
	xsetDead := func() *fakeRunner { return newFakeRunner().on("xset", stdout("", 1)) }
	xsetMissing := func() *fakeRunner { return newFakeRunner().on("xset", fails(errors.New("not found"))) }
	have := &fakePrompter{avail: true}
	noPrompter := &fakePrompter{avail: false}

	cases := []struct {
		name    string
		env     GUIEnv
		runner  *fakeRunner
		prompt  Prompter
		want    bool
		noXcall bool // xset must not be consulted
	}{
		{"wayland short-circuits xset", GUIEnv{WaylandDisplay: "wayland-0"}, xsetDead(), have, true, true},
		{"wayland but no prompter", GUIEnv{WaylandDisplay: "wayland-0"}, xsetOK(), noPrompter, false, true},
		{"x11 live server", GUIEnv{Display: ":0"}, xsetOK(), have, true, false},
		{"x11 dead server", GUIEnv{Display: ":0"}, xsetDead(), have, false, false},
		{"x11 no xset binary", GUIEnv{Display: ":0"}, xsetMissing(), have, false, false},
		{"no display at all", GUIEnv{}, xsetOK(), have, false, true},
		{"display ok but no prompter", GUIEnv{Display: ":0"}, xsetOK(), noPrompter, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GUIAvailable(c.env, c.runner, c.prompt); got != c.want {
				t.Fatalf("GUIAvailable = %v, want %v", got, c.want)
			}
			calledXset := false
			for _, call := range c.runner.calls {
				if call.Name == "xset" {
					calledXset = true
				}
			}
			if c.noXcall && calledXset {
				t.Fatal("xset was consulted but should have been short-circuited")
			}
		})
	}
}
