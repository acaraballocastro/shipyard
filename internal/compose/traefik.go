package compose

func BuildTraefikLabels(service string, domain string) []string {
	return []string{
		"traefik.enable=true",
		"traefik.http.routers." + service + ".rule=Host(`" + domain + "`)",
		"traefik.http.routers." + service + ".entrypoints=web",
		"traefik.http.services." + service + ".loadbalancer.server.port=80",
	}
}
