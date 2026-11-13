package config

import "testing"

func names(rs []Route) string {
	s := ""
	for _, r := range rs {
		s += r.Name + ","
	}
	return s
}

func TestEffectiveRoutes(t *testing.T) {
	cases := []struct {
		srv  Server
		want string
	}{
		{Server{Host: "pi", URL: "http://pi:7878"}, "lan,ssh,"},
		{Server{Host: "pi", URL: "http://pi:7878", NoSSHFallback: true}, "lan,"},
		{Server{Host: "pi"}, ""}, // API not enabled
		{Server{URL: "http://pi:7878"}, "lan,"},
		{Server{Host: "pi", URL: "http://pi:7878", Routes: []Route{{Name: "cf", URL: "https://x"}, {Name: "lan", URL: "http://pi:7878"}}}, "cf,lan,ssh,"},
	}
	for _, c := range cases {
		if got := names(c.srv.EffectiveRoutes()); got != c.want {
			t.Errorf("%+v: got %s want %s", c.srv, got, c.want)
		}
	}
}
